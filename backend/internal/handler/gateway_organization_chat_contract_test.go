package handler

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type organizationGatewayChatGeminiLimitUpstreamStub struct {
	accountIDs []int64
}

func (s *organizationGatewayChatGeminiLimitUpstreamStub) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	s.accountIDs = append(s.accountIDs, accountID)
	return &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"temporary upstream failure"}}`)),
	}, nil
}

func (s *organizationGatewayChatGeminiLimitUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, concurrency)
}

func TestOrganizationGatewayChatGeminiUsesGeminiAttemptLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 10
	cfg.Gateway.MaxAccountSwitchesGemini = 1
	enableOrganizationGatewayChatTestCapability(cfg)

	apiKey := organizationGatewayChatOrganizationAPIKey()
	apiKey.Group.Platform = service.PlatformGemini
	groupID := *apiKey.GroupID
	accounts := []service.Account{
		organizationGatewayChatGeminiLimitAccount(55, groupID, 1),
		organizationGatewayChatGeminiLimitAccount(66, groupID, 2),
		organizationGatewayChatGeminiLimitAccount(77, groupID, 3),
	}
	upstream := &organizationGatewayChatGeminiLimitUpstreamStub{}
	concurrency := service.NewConcurrencyService(organizationGatewayChatConcurrencyCacheStub{})
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewGatewayService(
		&organizationGatewayChatAccountRepoStub{accounts: accounts}, nil, nil, nil, nil, nil, nil, nil, cfg, nil,
		concurrency, billing, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	geminiCompat := service.NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, upstream, nil, cfg)
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, organizationGatewayChatRPMCacheStub{}, organizationGatewayChatUserGroupRateRepoStub{}, cfg, organizationGatewayChatQuotaRepoStub{})
	t.Cleanup(billingCache.Stop)
	executionRepo := &organizationGeminiPendingRepoStub{organizationResponsesExecutionRepoStub: &organizationResponsesExecutionRepoStub{}}
	handler := NewGatewayHandler(gateway, geminiCompat, nil, nil, concurrency, billingCache, nil, nil, nil, nil, nil, nil, cfg, nil)
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))

	body := []byte(`{"model":"gemini-3.1-pro","messages":[{"role":"user","content":"hello"}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.UserID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.ChatCompletions(c)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, executionRepo.reserveCommand.CandidateSnapshotJSON, `"maximum_attempts":2`)
	require.Empty(t, executionRepo.attemptCommands)
	require.Equal(t, int64(55), executionRepo.pending.AccountID)
	require.Equal(t, []int64{55}, upstream.accountIDs)
	require.Equal(t, 11, handler.organizationGatewayChatMaximumAttempts(service.PlatformAnthropic))
	require.Equal(t, 2, handler.organizationGatewayChatMaximumAttempts(service.PlatformGemini))
}

func organizationGatewayChatGeminiLimitAccount(id, groupID int64, priority int) service.Account {
	return service.Account{
		ID: id, Name: "gemini", Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 2, Priority: priority, GroupIDs: []int64{groupID}, AccountGroups: []service.AccountGroup{{GroupID: groupID}},
		Credentials: map[string]any{"api_key": "test-gemini-key"},
	}
}

func TestOrganizationGatewayChatAdapterRecordsUsageBeforeRetryingFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adapter := newOrganizationGatewayChatAdapterForContractTest()

	attempt, err := adapter.resultForForward(
		service.OrganizationExecutionDispatchRequest{},
		organizationGatewayChatContractCandidate(),
		&service.ForwardResult{Usage: service.ClaudeUsage{InputTokens: 2, OutputTokens: 1}},
		&service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable},
		adapter.c.Writer.Size(),
	)

	require.NoError(t, err)
	require.True(t, attempt.Dispatched)
	require.True(t, attempt.Retryable)
	require.False(t, attempt.PendingReconciliation)
	require.Equal(t, service.OrganizationExecutionTerminalOutcomeError, attempt.TerminalOutcome)
	require.NotEmpty(t, attempt.Observation.RawActualCost)
	require.NotEqual(t, "0", attempt.Observation.RawActualCost)
	require.EqualValues(t, 2, attempt.Observation.InputTokens)
	require.EqualValues(t, 1, attempt.Observation.OutputTokens)
}

func TestOrganizationGatewayChatAdapterRequiresZeroCostProofForRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("unproven upstream failure remains pending", func(t *testing.T) {
		adapter := newOrganizationGatewayChatAdapterForContractTest()
		attempt, err := adapter.resultForForward(
			service.OrganizationExecutionDispatchRequest{},
			organizationGatewayChatContractCandidate(),
			nil,
			&service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable},
			adapter.c.Writer.Size(),
		)

		require.Error(t, err)
		require.True(t, attempt.Dispatched)
		require.True(t, attempt.PendingReconciliation)
		require.False(t, attempt.Retryable)
	})

	t.Run("proven zero-cost upstream failure retries", func(t *testing.T) {
		adapter := newOrganizationGatewayChatAdapterForContractTest()
		attempt, err := adapter.resultForForward(
			service.OrganizationExecutionDispatchRequest{},
			organizationGatewayChatContractCandidate(),
			nil,
			&service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, ZeroCostProven: true},
			adapter.c.Writer.Size(),
		)

		require.NoError(t, err)
		require.True(t, attempt.Dispatched)
		require.True(t, attempt.Retryable)
		require.False(t, attempt.PendingReconciliation)
		require.Equal(t, "0", attempt.Observation.RawActualCost)
	})

	t.Run("transport failure remains pending", func(t *testing.T) {
		adapter := newOrganizationGatewayChatAdapterForContractTest()
		attempt, err := adapter.resultForForward(
			service.OrganizationExecutionDispatchRequest{},
			organizationGatewayChatContractCandidate(),
			nil,
			&service.UpstreamFailoverError{NoHTTPResponse: true, ZeroCostProven: true},
			adapter.c.Writer.Size(),
		)

		require.Error(t, err)
		require.True(t, attempt.Dispatched)
		require.True(t, attempt.PendingReconciliation)
		require.False(t, attempt.Retryable)
	})
}

func TestOrganizationGatewayChatAdapterDoesNotRetryAfterStreamingResponseCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adapter := newOrganizationGatewayChatAdapterForContractTest()
	writerSizeBefore := adapter.c.Writer.Size()
	_, err := adapter.c.Writer.WriteString("partial stream")
	require.NoError(t, err)

	attempt, err := adapter.resultForForward(
		service.OrganizationExecutionDispatchRequest{},
		organizationGatewayChatContractCandidate(),
		&service.ForwardResult{Usage: service.ClaudeUsage{InputTokens: 2, OutputTokens: 1}},
		&service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable},
		writerSizeBefore,
	)

	require.NoError(t, err)
	require.True(t, attempt.Dispatched)
	require.False(t, attempt.Retryable)
	require.False(t, attempt.PendingReconciliation)
	require.NotEqual(t, "0", attempt.Observation.RawActualCost)
}

func TestOrganizationGatewayChatAdapterTerminalizesTypedLocalPreDispatchError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adapter := newOrganizationGatewayChatAdapterForContractTest()

	attempt, err := adapter.resultForForward(
		service.OrganizationExecutionDispatchRequest{},
		organizationGatewayChatContractCandidate(),
		nil,
		&service.OpenAILocalPreDispatchError{Message: "missing upstream credential", ZeroProof: "local_policy_rejected"},
		0,
	)

	require.NoError(t, err)
	require.False(t, attempt.Dispatched)
	require.False(t, attempt.PendingReconciliation)
	require.Equal(t, "local_policy_rejected", attempt.ZeroProof)
}

func newOrganizationGatewayChatAdapterForContractTest() *organizationGatewayChatAdapter {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewGatewayService(
		nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, billing,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	return &organizationGatewayChatAdapter{
		handler:      &GatewayHandler{gatewayService: gateway},
		c:            c,
		requestModel: "claude-test",
	}
}

func organizationGatewayChatContractCandidate() service.OrganizationGatewayChatCandidate {
	return service.OrganizationGatewayChatCandidate{
		AccountID:      31,
		RequestedModel: "claude-test",
		DispatchModel:  "claude-test",
		UpstreamModel:  "claude-test",
		Route: service.OrganizationGatewayChatRoute{
			AccountID: 31,
			Platform:  service.PlatformAnthropic,
		},
		Pricing: service.OrganizationGeminiCandidatePricing{
			Model:             "claude-test",
			RateMultiplier:    1,
			AccountMultiplier: 1,
			Pricing: &service.ModelPricing{
				InputPricePerToken:  0.000001,
				OutputPricePerToken: 0.000002,
			},
		},
	}
}
