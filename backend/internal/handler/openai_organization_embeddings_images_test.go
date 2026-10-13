package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOrganizationEmbeddingsCapabilityRejectionCreatesZeroCostEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	executionRepo := &organizationResponsesExecutionRepoStub{}
	handler := NewOpenAIGatewayHandler(nil, nil, nil, nil, nil, nil, nil, cfg)
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))
	apiKey := organizationResponsesAuthorizedAPIKey()
	body := []byte(`{"model":"text-embedding-3-small","input":"hello"}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.Embeddings(c)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "ORGANIZATION_SPENDING_DISABLED")
	require.Equal(t, "0.00000000", executionRepo.reserveCommand.CandidateRouteMaximumCost)
	require.Equal(t, "request_policy_rejected:organization_spending_disabled", executionRepo.reserveCommand.PreDispatchZeroProof)
}

func TestOrganizationImagesInvalidRequestCreatesZeroCostEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, billing,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	executionRepo := &organizationResponsesExecutionRepoStub{}
	handler := NewOpenAIGatewayHandler(gateway, nil, nil, nil, nil, nil, nil, cfg)
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))
	apiKey := organizationResponsesAuthorizedAPIKey()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(`{"model":`))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.Images(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "0.00000000", executionRepo.reserveCommand.CandidateRouteMaximumCost)
	require.Equal(t, "request_policy_rejected:organization_embeddings_images_unsupported_request", executionRepo.reserveCommand.PreDispatchZeroProof)
}

func TestOrganizationImagesAdapterUnknownDispatchRemainsPendingWithEvidence(t *testing.T) {
	newAdapter := func() (*organizationImagesAdapter, service.OrganizationExecutionDispatchRequest, service.OrganizationEmbeddingsImagesCandidate) {
		return &organizationImagesAdapter{
				handler: &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}},
			}, service.OrganizationExecutionDispatchRequest{
				ProviderRequestID: "provider-request", ProviderCorrelationID: "provider-correlation",
			}, service.OrganizationEmbeddingsImagesCandidate{
				AccountID: 42, RequestedModel: "gpt-image-2", UpstreamModel: "gpt-image-2", Route: "openai_http_images",
			}
	}

	t.Run("zero image result is not a proven zero", func(t *testing.T) {
		adapter, request, candidate := newAdapter()
		result, err := adapter.resultForForward(request, candidate, &service.OpenAIForwardResult{RequestID: "upstream-request", ImageCount: 0}, nil, -1)

		require.ErrorIs(t, err, service.ErrOrganizationExecutionPendingReconciliation)
		require.True(t, result.Dispatched)
		require.True(t, result.PendingReconciliation)
		require.Equal(t, service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, result.TerminalOutcome)
		require.Empty(t, result.Observation.RawActualCost)
		require.Equal(t, "upstream-request", gjson.GetBytes(result.Observation.Evidence, "upstream_request_id").String())
		require.EqualValues(t, 0, gjson.GetBytes(result.Observation.Evidence, "image_count").Int())
	})

	t.Run("provider HTTP failover retries the next frozen candidate", func(t *testing.T) {
		adapter, request, candidate := newAdapter()
		forwardErr := &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: []byte(`{"error":{"message":"temporary upstream failure"}}`)}
		result, err := adapter.resultForForward(request, candidate, nil, forwardErr, -1)

		require.NoError(t, err)
		require.True(t, result.Dispatched)
		require.False(t, result.PendingReconciliation)
		require.True(t, result.Retryable)
		require.Equal(t, "0", result.Observation.RawActualCost)
		require.EqualValues(t, http.StatusBadGateway, gjson.GetBytes(result.Observation.Evidence, "upstream_status").Int())
	})

	t.Run("transport failover remains pending", func(t *testing.T) {
		adapter, request, candidate := newAdapter()
		forwardErr := &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway, NoHTTPResponse: true}
		result, err := adapter.resultForForward(request, candidate, nil, forwardErr, -1)

		require.ErrorIs(t, err, forwardErr)
		require.True(t, result.Dispatched)
		require.True(t, result.PendingReconciliation)
		require.False(t, result.Retryable)
		require.Empty(t, result.Observation.RawActualCost)
		require.EqualValues(t, http.StatusBadGateway, gjson.GetBytes(result.Observation.Evidence, "upstream_status").Int())
	})

	t.Run("ordinary upstream error is unknown until reconciliation", func(t *testing.T) {
		adapter, request, candidate := newAdapter()
		forwardErr := &service.OpenAIImagesUpstreamError{StatusCode: http.StatusBadRequest, Message: "invalid image request"}
		result, err := adapter.resultForForward(request, candidate, nil, forwardErr, -1)

		require.ErrorIs(t, err, forwardErr)
		require.True(t, result.Dispatched)
		require.True(t, result.PendingReconciliation)
		require.Empty(t, result.Observation.RawActualCost)
		require.EqualValues(t, http.StatusBadRequest, gjson.GetBytes(result.Observation.Evidence, "upstream_status").Int())
	})

	t.Run("local pre-dispatch proof still releases", func(t *testing.T) {
		adapter, request, candidate := newAdapter()
		result, err := adapter.resultForForward(request, candidate, nil, &service.OpenAILocalPreDispatchError{Message: "token unavailable"}, -1)

		require.NoError(t, err)
		require.False(t, result.Dispatched)
		require.False(t, result.PendingReconciliation)
		require.Equal(t, "local_policy_rejected", result.ZeroProof)
	})

	t.Run("untyped dispatch error cannot release", func(t *testing.T) {
		adapter, request, candidate := newAdapter()
		forwardErr := errors.New("transport outcome unknown")
		result, err := adapter.resultForForward(request, candidate, nil, forwardErr, -1)

		require.ErrorIs(t, err, forwardErr)
		require.True(t, result.Dispatched)
		require.True(t, result.PendingReconciliation)
	})
}

func TestOrganizationImagesConcurrencyRejectionCreatesZeroCostEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(42)
	imagePrice2K := 0.25
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.OrganizationSpending.Enabled = true
	cfg.Gateway.OrganizationSpending.Capabilities = map[string]map[string]bool{
		service.OrganizationEndpointFamilyImages: {
			service.OrganizationRequestShapeGenerations: true,
			service.OrganizationRequestShapeEdits:       true,
		},
	}
	cfg.Gateway.ImageConcurrency.Enabled = true
	cfg.Gateway.ImageConcurrency.MaxConcurrentRequests = 1
	cfg.Gateway.ImageConcurrency.OverflowMode = config.ImageConcurrencyOverflowModeReject
	account := service.Account{
		ID: 55, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive,
		Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID},
	}
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewOpenAIGatewayService(
		&organizationResponsesAccountRepoStub{account: account}, nil, nil, nil, nil, nil, nil, cfg, nil, nil, billing,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	executionRepo := &organizationResponsesExecutionRepoStub{}
	handler := NewOpenAIGatewayHandler(gateway, nil, nil, nil, nil, nil, nil, cfg)
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))
	apiKey := organizationResponsesAuthorizedAPIKey()
	apiKey.Group.AllowImageGeneration = true
	apiKey.Group.ImagePrice2K = &imagePrice2K

	body := []byte(`{"model":"gpt-image-2","prompt":"draw","n":1,"size":"2048x2048"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	release, acquired := handler.acquireImageGenerationSlot(c, false, false)
	require.True(t, acquired)
	require.NotNil(t, release)
	defer release()

	handler.Images(c)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, "rate_limit_error", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
	require.Equal(t, "0.00000000", executionRepo.reserveCommand.CandidateRouteMaximumCost)
	require.Equal(t, "request_policy_rejected:rate_limit_error", executionRepo.reserveCommand.PreDispatchZeroProof)
}

func TestOrganizationImagesTrustedFailoverUsesFrozenCandidateAndSettlesOnlyFinalAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(42)
	imagePrice2K := 0.25
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 1
	cfg.Gateway.OrganizationSpending.Enabled = true
	cfg.Gateway.OrganizationSpending.Capabilities = map[string]map[string]bool{
		service.OrganizationEndpointFamilyImages: {
			service.OrganizationRequestShapeGenerations: true,
			service.OrganizationRequestShapeEdits:       true,
		},
	}
	accounts := []service.Account{
		{
			ID: 55, Name: "first", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive,
			Schedulable: true, Concurrency: 1, Priority: 1, GroupIDs: []int64{groupID},
			Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
		},
		{
			ID: 66, Name: "second", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive,
			Schedulable: true, Concurrency: 1, Priority: 2, GroupIDs: []int64{groupID},
			Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
		},
	}
	upstream := &organizationImagesFailoverUpstreamStub{}
	concurrency := service.NewConcurrencyService(organizationResponsesConcurrencyCacheStub{})
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewOpenAIGatewayService(
		&organizationResponsesAccountRepoStub{accounts: accounts}, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, billing,
		nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil,
	)
	billingCache := service.NewBillingCacheService(
		nil, nil, nil, nil, organizationResponsesUserRPMCacheStub{}, organizationResponsesUserGroupRateRepoStub{}, cfg,
		organizationResponsesUserPlatformQuotaRepoStub{},
	)
	t.Cleanup(billingCache.Stop)
	executionRepo := &organizationResponsesExecutionRepoStub{}
	handler := NewOpenAIGatewayHandler(gateway, concurrency, billingCache, nil, nil, nil, nil, cfg)
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))
	apiKey := organizationResponsesAuthorizedAPIKey()
	apiKey.Group.AllowImageGeneration = true
	apiKey.Group.ImagePrice2K = &imagePrice2K

	body := []byte(`{"model":"gpt-image-2","prompt":"draw","n":1,"size":"2048x2048"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.Images(c)

	require.Equalf(t, http.StatusOK, recorder.Code, "response=%s reserve=%d attempts=%d", recorder.Body.String(), executionRepo.reserveCalls, len(executionRepo.attemptCommands))
	require.Equal(t, []int64{55, 66}, upstream.accountIDs)
	require.Equal(t, 1, executionRepo.reserveCalls)
	require.Len(t, executionRepo.attemptCommands, 2)
	require.Equal(t, int64(55), executionRepo.attemptCommands[0].AccountID)
	require.Equal(t, int64(66), executionRepo.attemptCommands[1].AccountID)
	require.Equal(t, service.OrganizationExecutionTerminalOutcomeSuccess, executionRepo.terminalCommand.TerminalOutcome)
	require.NotNil(t, executionRepo.terminalCommand.FinalAttemptID)
	require.Equal(t, executionRepo.attemptCommands[1].AttemptID, *executionRepo.terminalCommand.FinalAttemptID)
	require.NotEqual(t, executionRepo.attemptCommands[0].AttemptID, *executionRepo.terminalCommand.FinalAttemptID)
	require.Equal(t, "0", executionRepo.attemptCommands[0].RawActualCost)
	require.NotEqual(t, "0", executionRepo.attemptCommands[1].RawActualCost)
}

func TestOrganizationImagesUnknownDispatchRecordsPendingAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(42)
	imagePrice2K := 0.25
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.OrganizationSpending.Enabled = true
	cfg.Gateway.OrganizationSpending.Capabilities = map[string]map[string]bool{
		service.OrganizationEndpointFamilyImages: {
			service.OrganizationRequestShapeGenerations: true,
			service.OrganizationRequestShapeEdits:       true,
		},
	}
	account := service.Account{
		ID: 55, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive,
		Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID},
		Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
	}
	concurrency := service.NewConcurrencyService(organizationResponsesConcurrencyCacheStub{})
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewOpenAIGatewayService(
		&organizationResponsesAccountRepoStub{account: account}, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, billing,
		nil, nil, organizationImagesUnknownUpstreamStub{}, nil, nil, nil, nil, nil, nil, nil,
	)
	billingCache := service.NewBillingCacheService(
		nil, nil, nil, nil, organizationResponsesUserRPMCacheStub{}, organizationResponsesUserGroupRateRepoStub{}, cfg,
		organizationResponsesUserPlatformQuotaRepoStub{},
	)
	t.Cleanup(billingCache.Stop)
	executionRepo := &organizationImagesPendingExecutionRepoStub{}
	handler := NewOpenAIGatewayHandler(gateway, concurrency, billingCache, nil, nil, nil, nil, cfg)
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))
	apiKey := organizationResponsesAuthorizedAPIKey()
	apiKey.Group.AllowImageGeneration = true
	apiKey.Group.ImagePrice2K = &imagePrice2K

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(`{"model":"gpt-image-2","prompt":"draw","n":1,"size":"2048x2048"}`))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.Images(c)

	require.Equalf(t, http.StatusConflict, recorder.Code, "response=%s reserve=%d pending=%d", recorder.Body.String(), executionRepo.reserveCalls, executionRepo.pendingCalls)
	require.Equal(t, 1, executionRepo.reserveCalls)
	require.Len(t, executionRepo.attemptCommands, 0)
	require.Equal(t, 0, executionRepo.terminalizeCalls)
	require.Equal(t, 1, executionRepo.pendingCalls)
	require.NotEmpty(t, executionRepo.pendingCommand.ProviderRequestID)
	require.NotEmpty(t, executionRepo.pendingCommand.ProviderCorrelationID)
	require.Equal(t, int64(55), executionRepo.pendingCommand.AccountID)
	require.Equal(t, "openai_http_images", gjson.Get(executionRepo.pendingCommand.RouteSnapshotJSON, "route").String())
	require.Equal(t, "gpt-image-2", gjson.Get(executionRepo.pendingCommand.RouteSnapshotJSON, "upstream_model").String())
	require.NotEmpty(t, executionRepo.pendingCommand.RouteSnapshotHash)
	require.NotEmpty(t, executionRepo.pendingCommand.PricingSnapshotHash)
	require.False(t, executionRepo.pendingCommand.DispatchedAt.IsZero())
}

func TestOrganizationEmbeddingsObservedUsageSettlesOneFrozenExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(42)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	enableOrganizationEmbeddingsTestCapability(cfg)
	account := service.Account{
		ID: 55, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive,
		Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID},
		Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
	}
	upstream := &organizationEmbeddingsUpstreamStub{responses: []*http.Response{organizationEmbeddingsResponse(http.StatusOK, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"usage":{"prompt_tokens":3,"total_tokens":3}}`)}}
	concurrency := service.NewConcurrencyService(organizationResponsesConcurrencyCacheStub{})
	billing := organizationEmbeddingsTestBilling(t, cfg)
	gateway := service.NewOpenAIGatewayService(
		&organizationResponsesAccountRepoStub{account: account}, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, billing,
		nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil,
	)
	billingCache := organizationEmbeddingsTestBillingCache(cfg)
	t.Cleanup(billingCache.Stop)
	executionRepo := &organizationResponsesExecutionRepoStub{}
	handler := NewOpenAIGatewayHandler(gateway, concurrency, billingCache, nil, nil, nil, nil, cfg)
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))
	apiKey := organizationResponsesAuthorizedAPIKey()
	body := []byte(`{"model":"text-embedding-3-small","input":"hello"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.Embeddings(c)

	require.Equalf(t, http.StatusOK, recorder.Code, "response=%s", recorder.Body.String())
	require.Equal(t, 1, executionRepo.reserveCalls)
	require.Len(t, executionRepo.attemptCommands, 1)
	require.Equal(t, service.OrganizationExecutionTerminalOutcomeSuccess, executionRepo.terminalCommand.TerminalOutcome)
	require.NotNil(t, executionRepo.terminalCommand.FinalAttemptID)
	require.Equal(t, executionRepo.attemptCommands[0].AttemptID, *executionRepo.terminalCommand.FinalAttemptID)
	require.NotEqual(t, "0", executionRepo.attemptCommands[0].RawActualCost)
	require.Equal(t, int64(55), executionRepo.attemptCommands[0].AccountID)
	require.Equal(t, int64(1), gjson.Get(executionRepo.reserveCommand.CandidateSnapshotJSON, "candidates.#").Int())
}

func TestOrganizationEmbeddingsTrustedFailoverUsesFrozenCandidatesAndFinalAttemptOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(42)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 1
	enableOrganizationEmbeddingsTestCapability(cfg)
	accounts := []service.Account{
		{ID: 55, Name: "first", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1, GroupIDs: []int64{groupID}, Credentials: map[string]any{"api_key": "first-key", "base_url": "https://api.openai.com"}},
		{ID: 66, Name: "second", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 2, GroupIDs: []int64{groupID}, Credentials: map[string]any{"api_key": "second-key", "base_url": "https://api.openai.com"}},
	}
	upstream := &organizationEmbeddingsUpstreamStub{responses: []*http.Response{
		organizationEmbeddingsResponse(http.StatusBadGateway, `{"error":{"message":"temporary upstream failure"}}`),
		organizationEmbeddingsResponse(http.StatusOK, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"usage":{"prompt_tokens":3,"total_tokens":3}}`),
	}}
	concurrency := service.NewConcurrencyService(organizationResponsesConcurrencyCacheStub{})
	billing := organizationEmbeddingsTestBilling(t, cfg)
	gateway := service.NewOpenAIGatewayService(
		&organizationResponsesAccountRepoStub{accounts: accounts}, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, billing,
		nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil,
	)
	billingCache := organizationEmbeddingsTestBillingCache(cfg)
	t.Cleanup(billingCache.Stop)
	executionRepo := &organizationResponsesExecutionRepoStub{}
	handler := NewOpenAIGatewayHandler(gateway, concurrency, billingCache, nil, nil, nil, nil, cfg)
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))
	apiKey := organizationResponsesAuthorizedAPIKey()
	body := []byte(`{"model":"text-embedding-3-small","input":"hello"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.Embeddings(c)

	require.Equalf(t, http.StatusOK, recorder.Code, "response=%s", recorder.Body.String())
	require.Equal(t, []int64{55, 66}, upstream.accountIDs)
	require.Equal(t, 1, executionRepo.reserveCalls)
	require.Len(t, executionRepo.attemptCommands, 2)
	require.Equal(t, int64(2), gjson.Get(executionRepo.reserveCommand.CandidateSnapshotJSON, "candidates.#").Int())
	require.Equal(t, "0", executionRepo.attemptCommands[0].RawActualCost)
	require.NotEqual(t, "0", executionRepo.attemptCommands[1].RawActualCost)
	require.NotNil(t, executionRepo.terminalCommand.FinalAttemptID)
	require.Equal(t, executionRepo.attemptCommands[1].AttemptID, *executionRepo.terminalCommand.FinalAttemptID)
	require.NotEqual(t, executionRepo.attemptCommands[0].AttemptID, *executionRepo.terminalCommand.FinalAttemptID)
}

func TestOrganizationEmbeddingsUnknownTransportRecordsPendingEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(42)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	enableOrganizationEmbeddingsTestCapability(cfg)
	account := service.Account{
		ID: 55, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive,
		Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID},
		Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
	}
	upstream := &organizationEmbeddingsUpstreamStub{err: errors.New("transport outcome unknown")}
	concurrency := service.NewConcurrencyService(organizationResponsesConcurrencyCacheStub{})
	billing := organizationEmbeddingsTestBilling(t, cfg)
	gateway := service.NewOpenAIGatewayService(
		&organizationResponsesAccountRepoStub{account: account}, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, billing,
		nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil,
	)
	billingCache := organizationEmbeddingsTestBillingCache(cfg)
	t.Cleanup(billingCache.Stop)
	executionRepo := &organizationImagesPendingExecutionRepoStub{}
	handler := NewOpenAIGatewayHandler(gateway, concurrency, billingCache, nil, nil, nil, nil, cfg)
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))
	apiKey := organizationResponsesAuthorizedAPIKey()
	body := []byte(`{"model":"text-embedding-3-small","input":"hello"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.Embeddings(c)

	require.Equalf(t, http.StatusBadGateway, recorder.Code, "response=%s", recorder.Body.String())
	require.Equal(t, "upstream_error", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
	require.Equal(t, 1, executionRepo.reserveCalls)
	require.Empty(t, executionRepo.attemptCommands)
	require.Equal(t, 1, executionRepo.pendingCalls)
	require.Equal(t, int64(55), executionRepo.pendingCommand.AccountID)
	require.Equal(t, "openai_http_embeddings", gjson.Get(executionRepo.pendingCommand.RouteSnapshotJSON, "route").String())
	require.NotEmpty(t, executionRepo.pendingCommand.ProviderRequestID)
	require.NotEmpty(t, executionRepo.pendingCommand.ProviderCorrelationID)
}

func enableOrganizationEmbeddingsTestCapability(cfg *config.Config) {
	cfg.Gateway.OrganizationSpending.Enabled = true
	cfg.Gateway.OrganizationSpending.Capabilities = map[string]map[string]bool{
		service.OrganizationEndpointFamilyEmbeddings: {
			service.OrganizationRequestShapeHTTP: true,
		},
	}
}

func organizationEmbeddingsTestBillingCache(cfg *config.Config) *service.BillingCacheService {
	return service.NewBillingCacheService(
		nil, nil, nil, nil, organizationResponsesUserRPMCacheStub{}, organizationResponsesUserGroupRateRepoStub{}, cfg,
		organizationResponsesUserPlatformQuotaRepoStub{},
	)
}

func organizationEmbeddingsTestBilling(t *testing.T, cfg *config.Config) *service.BillingService {
	t.Helper()
	cfg.Pricing.DataDir = t.TempDir()
	cfg.Pricing.RemoteURL = "https://pricing.example.test/model_prices.json"
	pricing := service.NewPricingService(cfg, organizationEmbeddingsPricingRemoteStub{})
	require.NoError(t, pricing.Initialize())
	t.Cleanup(pricing.Stop)
	return service.NewBillingService(cfg, pricing)
}

type organizationEmbeddingsPricingRemoteStub struct{}

func (organizationEmbeddingsPricingRemoteStub) FetchPricingJSON(context.Context, string) ([]byte, error) {
	return []byte(`{"text-embedding-3-small":{"input_cost_per_token":0.000001,"litellm_provider":"openai","mode":"embedding"}}`), nil
}

func (organizationEmbeddingsPricingRemoteStub) FetchHashText(context.Context, string) (string, error) {
	return "", nil
}

type organizationEmbeddingsUpstreamStub struct {
	accountIDs []int64
	responses  []*http.Response
	err        error
}

func (u *organizationEmbeddingsUpstreamStub) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.accountIDs = append(u.accountIDs, accountID)
	if u.err != nil {
		return nil, u.err
	}
	if len(u.responses) == 0 {
		return nil, errors.New("unexpected embeddings upstream request")
	}
	response := u.responses[0]
	u.responses = u.responses[1:]
	return response, nil
}

func (u *organizationEmbeddingsUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func organizationEmbeddingsResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"embeddings_upstream"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

type organizationImagesFailoverUpstreamStub struct {
	accountIDs []int64
}

func (u *organizationImagesFailoverUpstreamStub) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.accountIDs = append(u.accountIDs, accountID)
	if len(u.accountIDs) == 1 {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"message":"temporary upstream failure"}}`)),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"img_final"}},
		Body:       io.NopCloser(bytes.NewBufferString(`{"created":1,"data":[{"url":"https://example.invalid/image.png"}]}`)),
	}, nil
}

func (u *organizationImagesFailoverUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

type organizationImagesUnknownUpstreamStub struct{}

func (organizationImagesUnknownUpstreamStub) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return nil, errors.New("transport outcome unknown")
}

func (u organizationImagesUnknownUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

type organizationImagesPendingExecutionRepoStub struct {
	organizationResponsesExecutionRepoStub
	pendingCommand service.OrganizationPendingAttemptCommand
	pendingCalls   int
}

func (r *organizationImagesPendingExecutionRepoStub) RecordPendingAttempt(_ context.Context, command service.OrganizationPendingAttemptCommand) error {
	r.pendingCalls++
	r.pendingCommand = command
	return nil
}

func (*organizationImagesPendingExecutionRepoStub) ReconcilePendingExecution(context.Context, service.OrganizationPendingReconciliationCommand) (*service.OrganizationFinalExecutionEvidenceResult, error) {
	return nil, service.ErrOrganizationExecutionPendingReconciliation
}

func (*organizationImagesPendingExecutionRepoStub) ListPendingReconciliations(context.Context, int64, service.OrganizationPendingReconciliationListParams) ([]service.OrganizationPendingReconciliation, *service.OrganizationPendingReconciliationPage, error) {
	return nil, nil, nil
}

func (*organizationImagesPendingExecutionRepoStub) GetPendingReconciliation(context.Context, int64, string) (*service.OrganizationPendingReconciliation, error) {
	return nil, nil
}

func (*organizationImagesPendingExecutionRepoStub) RedriveAttemptAccountEffect(context.Context, string) (*service.OrganizationAttemptAccountEffectResult, error) {
	return nil, nil
}

func (*organizationImagesPendingExecutionRepoStub) RedrivePendingAttemptAccountEffects(context.Context, int) ([]service.OrganizationAttemptAccountEffectResult, error) {
	return nil, nil
}

func (*organizationImagesPendingExecutionRepoStub) RedriveExecutionOutbox(context.Context, int64) (*service.OrganizationExecutionOutboxApplyResult, error) {
	return nil, nil
}
