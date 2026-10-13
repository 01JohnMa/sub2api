package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type organizationNonBillableHandlerRepoStub struct {
	*organizationResponsesExecutionRepoStub
	beginCommand    service.OrganizationNonBillableExecutionCommand
	attemptCommand  service.OrganizationNonBillableAttemptEvidenceCommand
	pendingCommand  service.OrganizationNonBillablePendingAttemptCommand
	terminalCommand service.OrganizationNonBillableTerminalizationCommand
}

func (r *organizationNonBillableHandlerRepoStub) BeginNonBillableExecution(_ context.Context, command service.OrganizationNonBillableExecutionCommand) (*service.OrganizationNonBillableExecutionStartResult, error) {
	r.beginCommand = command
	return &service.OrganizationNonBillableExecutionStartResult{
		ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
		OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID, APIKeyID: command.Attribution.APIKeyID,
		AllocationID: command.Attribution.AllocationID,
		State:        service.OrganizationExecutionStateAccepted, Disposition: service.OrganizationNonBillableExecutionDispositionStarted,
	}, nil
}

func (r *organizationNonBillableHandlerRepoStub) RecordNonBillableAttemptEvidence(_ context.Context, command service.OrganizationNonBillableAttemptEvidenceCommand) (*service.OrganizationAttemptEvidenceResult, error) {
	r.attemptCommand = command
	return &service.OrganizationAttemptEvidenceResult{AttemptEvidenceID: 601, ExecutionID: command.ExecutionID, AttemptID: command.AttemptID}, nil
}

func (r *organizationNonBillableHandlerRepoStub) RecordNonBillablePendingAttempt(_ context.Context, command service.OrganizationNonBillablePendingAttemptCommand) error {
	r.pendingCommand = command
	return nil
}

func (r *organizationNonBillableHandlerRepoStub) TerminalizeNonBillableExecution(_ context.Context, command service.OrganizationNonBillableTerminalizationCommand) (*service.OrganizationNonBillableFinalExecutionEvidenceResult, error) {
	r.terminalCommand = command
	result := &service.OrganizationNonBillableFinalExecutionEvidenceResult{
		FinalEvidenceID: 701, ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
		OrganizationID: command.OrganizationID, UserID: command.UserID, APIKeyID: command.APIKeyID,
		AllocationID:   command.AllocationID,
		FinalAttemptID: command.FinalAttemptID, FinalAttemptEvidenceID: command.FinalAttemptEvidenceID,
		TerminalOutcome: command.TerminalOutcome, BusinessLimited: command.BusinessLimited,
		RawActualCost: "0", OrganizationSettledCost: "0.00000000", ZeroProof: command.ZeroProof,
		IsCountTokensProbe: r.beginCommand.EndpointFamily == service.OrganizationEndpointFamilyTokenCounting,
		OccurredAt:         command.OccurredAt,
	}
	if command.FinalAttemptID != nil {
		result.InputTokens = r.attemptCommand.InputTokens
		result.OutputTokens = r.attemptCommand.OutputTokens
		result.CacheCreationTokens = r.attemptCommand.CacheCreationTokens
		result.CacheReadTokens = r.attemptCommand.CacheReadTokens
	}
	return result, nil
}

func TestOrganizationOpenAICountTokensCreatesLocalZeroCostEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Gateway.OrganizationSpending.Enabled = true
	cfg.Gateway.OrganizationSpending.Capabilities = map[string]map[string]bool{
		service.OrganizationEndpointFamilyTokenCounting: {service.OrganizationRequestShapeProbe: true},
	}
	apiKey := organizationGatewayChatOrganizationAPIKey()
	apiKey.Group.Platform = service.PlatformOpenAI
	apiKey.User.Balance = 0
	apiKey.OrganizationAuthorization.Available = "0.00000000"
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, organizationGatewayChatRPMCacheStub{}, organizationGatewayChatUserGroupRateRepoStub{}, cfg, organizationGatewayChatQuotaRepoStub{})
	t.Cleanup(billingCache.Stop)
	repo := &organizationNonBillableHandlerRepoStub{organizationResponsesExecutionRepoStub: &organizationResponsesExecutionRepoStub{}}
	handler := &GatewayHandler{billingCacheService: billingCache, organizationExecution: service.NewOrganizationExecutionService(repo), cfg: cfg}
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := service.ParseGatewayRequest(service.NewRequestBodyRef(body), domain.PlatformAnthropic)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.UserID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.handleOrganizationCountTokens(c, apiKey, parsed, zap.NewNop())

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Equal(t, service.OrganizationEndpointFamilyTokenCounting, repo.beginCommand.EndpointFamily)
	require.Equal(t, service.OrganizationExecutionTerminalOutcomeError, repo.terminalCommand.TerminalOutcome)
	require.Equal(t, "openai_count_tokens_unsupported", repo.terminalCommand.ZeroProof)
	require.True(t, repo.terminalCommand.BusinessLimited)
	require.Empty(t, repo.attemptCommand.AttemptID)
	require.Empty(t, repo.pendingCommand.AttemptID)
}

func TestOrganizationCountTokensValidationErrorsCreateProbeEvidence(t *testing.T) {
	tests := []struct {
		name  string
		body  []byte
		proof string
	}{
		{name: "empty body", body: nil, proof: "count_tokens_body_empty"},
		{name: "malformed body", body: []byte(`{"model":`), proof: "count_tokens_body_invalid"},
		{name: "missing model", body: []byte(`{"messages":[]}`), proof: "count_tokens_model_missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, apiKey, repo, cleanup := newOrganizationNonBillableHandlerTest(t, service.PlatformOpenAI)
			defer cleanup()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", bytes.NewReader(tt.body))
			c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.UserID, Concurrency: apiKey.User.Concurrency})
			c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

			handler.CountTokens(c)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Equal(t, service.OrganizationEndpointFamilyTokenCounting, repo.beginCommand.EndpointFamily)
			require.Equal(t, tt.proof, repo.terminalCommand.ZeroProof)
			require.Equal(t, service.OrganizationExecutionTerminalOutcomeError, repo.terminalCommand.TerminalOutcome)
			require.Empty(t, repo.attemptCommand.AttemptID)
		})
	}
}

func TestOrganizationGeminiCountTokensEmptyBodyCreatesProbeEvidence(t *testing.T) {
	handler, apiKey, repo, cleanup := newOrganizationNonBillableHandlerTest(t, service.PlatformGemini)
	defer cleanup()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:countTokens", nil)
	c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-2.5-pro:countTokens"}}
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.UserID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.GeminiV1BetaModels(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, service.OrganizationEndpointFamilyTokenCounting, repo.beginCommand.EndpointFamily)
	require.Equal(t, "gemini_count_tokens_body_empty", repo.terminalCommand.ZeroProof)
	require.True(t, repo.terminalCommand.BusinessLimited)
}

func TestOrganizationUsageRejectsInvalidDaysWithZeroCostErrorEvidence(t *testing.T) {
	handler, apiKey, repo, cleanup := newOrganizationNonBillableHandlerTest(t, service.PlatformOpenAI)
	defer cleanup()
	handler.cfg.Gateway.OrganizationSpending.Capabilities[service.OrganizationEndpointFamilyUsage] = map[string]bool{service.OrganizationRequestShapeHTTP: true}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage?days=0", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.UserID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.Usage(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, service.OrganizationEndpointFamilyUsage, repo.beginCommand.EndpointFamily)
	require.Equal(t, service.OrganizationExecutionTerminalOutcomeError, repo.terminalCommand.TerminalOutcome)
	require.Equal(t, "usage_invalid_days", repo.terminalCommand.ZeroProof)
	require.True(t, repo.terminalCommand.BusinessLimited)
	require.Empty(t, repo.attemptCommand.AttemptID)
}

func TestOrganizationUsageRunsAdmissionBeforeInvalidDaysValidation(t *testing.T) {
	handler, apiKey, repo, cleanup := newOrganizationNonBillableHandlerTest(t, service.PlatformOpenAI)
	defer cleanup()
	handler.cfg.Gateway.OrganizationSpending.Capabilities[service.OrganizationEndpointFamilyUsage] = map[string]bool{service.OrganizationRequestShapeHTTP: true}
	apiKey.OrganizationAuthorization.OrganizationStatus = "suspended"
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage?days=0", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.UserID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.Usage(c)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Empty(t, repo.beginCommand.ExecutionID)
}

func TestOrganizationGeminiModelsUnavailableCreatesZeroCostErrorEvidence(t *testing.T) {
	handler, apiKey, repo, cleanup := newOrganizationNonBillableHandlerTest(t, service.PlatformGemini)
	defer cleanup()
	handler.cfg.Gateway.OrganizationSpending.Capabilities[service.OrganizationEndpointFamilyModels] = map[string]bool{service.OrganizationRequestShapeHTTP: true}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.UserID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.handleOrganizationGeminiModelsRead(c, apiKey, "")

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "gemini_models_service_unavailable", repo.terminalCommand.ZeroProof)
	require.Equal(t, service.OrganizationExecutionTerminalOutcomeError, repo.terminalCommand.TerminalOutcome)
}

func TestParseOrganizationCountTokensPreservesGeminiCacheTokens(t *testing.T) {
	input, output, cacheCreation, cacheRead := parseOrganizationCountTokens([]byte(`{"totalTokens":19,"cachedContentTokenCount":7}`))
	require.EqualValues(t, 12, input)
	require.Zero(t, output)
	require.Zero(t, cacheCreation)
	require.EqualValues(t, 7, cacheRead)
}

func TestOrganizationNonBillableResponseCaptureDoesNotTruncateClientResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	capture := newOrganizationNonBillableResponseCapture(c.Writer)
	payload := bytes.Repeat([]byte("x"), organizationNonBillableResponseCaptureLimit+257)

	written, err := capture.Write(payload)

	require.NoError(t, err)
	require.Equal(t, len(payload), written)
	require.Empty(t, recorder.Body.Bytes())
	require.Len(t, capture.body.Bytes(), organizationNonBillableResponseCaptureLimit)
	require.NoError(t, capture.commit())
	require.Equal(t, payload, recorder.Body.Bytes())
}

func TestOrganizationNonBillableResponseWaitsForDurableFinalBeforeFlush(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	capture := newOrganizationNonBillableResponseCapture(c.Writer)
	_, err := capture.Write([]byte(`{"ok":true}`))
	require.NoError(t, err)

	// A persistence failure returns no final evidence, so the provider response
	// must remain buffered and the handler can still emit a gateway error.
	require.NoError(t, commitOrganizationNonBillableResponse(nil, capture))
	require.Empty(t, recorder.Body.Bytes())

	require.NoError(t, commitOrganizationNonBillableResponse(&service.OrganizationNonBillableFinalExecutionEvidenceResult{}, capture))
	require.Equal(t, `{"ok":true}`, recorder.Body.String())
}

func TestOrganizationNonBillablePendingObservationHasUnknownCostAndNoTokens(t *testing.T) {
	account := &service.Account{ID: 55, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey}
	observation := organizationNonBillablePendingObservation(service.OrganizationNonBillableDispatchRequest{
		EndpointFamily:        service.OrganizationEndpointFamilyTokenCounting,
		RequestShape:          service.OrganizationRequestShapeProbe,
		ProviderRequestID:     "provider-request",
		ProviderCorrelationID: "provider-correlation",
	}, account, http.StatusBadGateway, []byte(`{"error":"transport"}`))

	require.NotNil(t, observation)
	require.Empty(t, observation.RawActualCost)
	require.Zero(t, observation.InputTokens)
	require.Zero(t, observation.OutputTokens)
	require.Zero(t, observation.CacheCreationTokens)
	require.Zero(t, observation.CacheReadTokens)
}

func newOrganizationNonBillableHandlerTest(t *testing.T, platform string) (*GatewayHandler, *service.APIKey, *organizationNonBillableHandlerRepoStub, func()) {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Gateway.OrganizationSpending.Enabled = true
	cfg.Gateway.OrganizationSpending.Capabilities = map[string]map[string]bool{
		service.OrganizationEndpointFamilyTokenCounting: {service.OrganizationRequestShapeProbe: true},
	}
	apiKey := organizationGatewayChatOrganizationAPIKey()
	apiKey.Group.Platform = platform
	apiKey.User.Balance = 0
	apiKey.OrganizationAuthorization.Available = "0.00000000"
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, organizationGatewayChatRPMCacheStub{}, organizationGatewayChatUserGroupRateRepoStub{}, cfg, organizationGatewayChatQuotaRepoStub{})
	repo := &organizationNonBillableHandlerRepoStub{organizationResponsesExecutionRepoStub: &organizationResponsesExecutionRepoStub{}}
	handler := &GatewayHandler{billingCacheService: billingCache, organizationExecution: service.NewOrganizationExecutionService(repo), cfg: cfg}
	return handler, apiKey, repo, billingCache.Stop
}
