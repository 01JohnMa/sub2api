package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOrganizationChatPreDispatchRejectionCreatesChatZeroCostEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	handler := NewOpenAIGatewayHandler(nil, nil, nil, nil, nil, nil, nil, cfg)
	executionRepo := &organizationResponsesExecutionRepoStub{}
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"gpt-5.4","messages":[]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	streamStarted := false

	ok := handler.recordOrganizationChatPreDispatchRejection(
		c,
		organizationResponsesAuthorizedAPIKey(),
		body,
		service.OrganizationRequestShapeHTTP,
		service.ErrOrganizationChatUnsupportedRequest,
		&streamStarted,
	)

	require.True(t, ok)
	require.Equal(t, service.OrganizationEndpointFamilyChat, executionRepo.reserveCommand.EndpointFamily)
	require.Equal(t, "0.00000000", executionRepo.reserveCommand.CandidateRouteMaximumCost)
	require.Contains(t, executionRepo.reserveCommand.PreDispatchZeroProof, "organization_chat_unsupported_request")
	require.False(t, recorder.Flushed)
}

type organizationOpenAIChatFallbackUpstreamStub struct {
	requests []*http.Request
}

func (u *organizationOpenAIChatFallbackUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.requests = append(u.requests, req.Clone(req.Context()))
	if len(u.requests) == 1 {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"error":{"message":"responses endpoint not found"},"usage":{"prompt_tokens":0,"completion_tokens":0}}`,
			)),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"chat_raw_2"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_2","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		)),
	}, nil
}

func (u *organizationOpenAIChatFallbackUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestOrganizationOpenAIChatUnknownResponsesFallbackUsesFrozenAttempts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 1
	enableOrganizationGatewayChatTestCapability(cfg)
	groupID := int64(42)
	account := service.Account{
		ID: 55, Name: "unknown responses", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{groupID},
		Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeAuto)},
	}
	upstream := &organizationOpenAIChatFallbackUpstreamStub{}
	concurrency := service.NewConcurrencyService(organizationResponsesConcurrencyCacheStub{})
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewOpenAIGatewayService(
		&organizationResponsesAccountRepoStub{account: account}, nil, nil, nil, nil, nil, nil, cfg, nil,
		concurrency, billing, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil,
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
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":64}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.ChatCompletions(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "/v1/responses", upstream.requests[0].URL.Path)
	require.Equal(t, "/v1/chat/completions", upstream.requests[1].URL.Path)
	require.Equal(t, 1, executionRepo.reserveCalls)
	require.Len(t, executionRepo.attemptCommands, 2)
	require.Equal(t, "0", executionRepo.attemptCommands[0].RawActualCost)
	require.NotEqual(t, "0", executionRepo.attemptCommands[1].RawActualCost)
	require.NotNil(t, executionRepo.terminalCommand.FinalAttemptID)
	require.Equal(t, executionRepo.attemptCommands[1].AttemptID, *executionRepo.terminalCommand.FinalAttemptID)
}

type organizationOpenAIChatUsageFailoverUpstreamStub struct {
	accountIDs []int64
}

type organizationOpenAIChatResponsesUpstreamStub struct{}

func (organizationOpenAIChatResponsesUpstreamStub) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body := strings.Join([]string{
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"id":"resp_chat_1","object":"response","model":"gpt-5.4","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":2,"input_tokens_details":{"cached_tokens":1}}}}`,
		``,
	}, "\n")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"responses_chat_1"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func (u organizationOpenAIChatResponsesUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestOrganizationOpenAIChatResponsesBridgeSettlesObservedUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	enableOrganizationGatewayChatTestCapability(cfg)
	groupID := int64(42)
	account := service.Account{
		ID: 55, Name: "responses", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{groupID},
		Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses),
		},
	}
	concurrency := service.NewConcurrencyService(organizationResponsesConcurrencyCacheStub{})
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewOpenAIGatewayService(
		&organizationResponsesAccountRepoStub{account: account}, nil, nil, nil, nil, nil, nil, cfg, nil,
		concurrency, billing, nil, nil, organizationOpenAIChatResponsesUpstreamStub{}, nil, nil, nil, nil, nil, nil, nil,
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
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":64}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.ChatCompletions(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, executionRepo.attemptCommands, 1)
	require.NotEqual(t, "0", executionRepo.attemptCommands[0].RawActualCost)
	require.NotNil(t, executionRepo.terminalCommand.FinalAttemptID)
	require.Equal(t, executionRepo.attemptCommands[0].AttemptID, *executionRepo.terminalCommand.FinalAttemptID)
}

func (u *organizationOpenAIChatUsageFailoverUpstreamStub) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.accountIDs = append(u.accountIDs, accountID)
	if len(u.accountIDs) == 1 {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"failed_with_usage"}},
			Body: io.NopCloser(strings.NewReader(
				`{"error":{"message":"temporary failure"},"usage":{"prompt_tokens":4,"completion_tokens":1}}`,
			)),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"chat_success_2"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_2","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		)),
	}, nil
}

func (u *organizationOpenAIChatUsageFailoverUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestOrganizationOpenAIChatFailoverUsageRecordsPlatformAttemptCost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 1
	enableOrganizationGatewayChatTestCapability(cfg)
	groupID := int64(42)
	accounts := []service.Account{
		organizationOpenAIChatRawAccount(55, groupID, 1),
		organizationOpenAIChatRawAccount(66, groupID, 2),
	}
	upstream := &organizationOpenAIChatUsageFailoverUpstreamStub{}
	concurrency := service.NewConcurrencyService(organizationResponsesConcurrencyCacheStub{})
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewOpenAIGatewayService(
		&organizationResponsesAccountRepoStub{accounts: accounts}, nil, nil, nil, nil, nil, nil, cfg, nil,
		concurrency, billing, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil,
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
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":64}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.ChatCompletions(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []int64{55, 66}, upstream.accountIDs)
	require.Len(t, executionRepo.attemptCommands, 2)
	require.NotEqual(t, "0", executionRepo.attemptCommands[0].RawActualCost)
	require.NotEqual(t, "0", executionRepo.attemptCommands[1].RawActualCost)
	require.NotNil(t, executionRepo.terminalCommand.FinalAttemptID)
	require.Equal(t, executionRepo.attemptCommands[1].AttemptID, *executionRepo.terminalCommand.FinalAttemptID)
}

func TestOrganizationOpenAIChatHTTPFailoverWithoutUsageIsPending(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, statusCode := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusTooManyRequests, http.StatusBadGateway} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			adapter := &organizationChatCompletionsAdapter{
				handler: &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}},
				c:       c,
			}

			result, err := adapter.resultForForward(
				service.OrganizationExecutionDispatchRequest{AttemptID: "attempt_unknown", AttemptSequence: 1},
				service.OrganizationChatCandidate{AccountID: 55},
				nil,
				&service.UpstreamFailoverError{StatusCode: statusCode},
				0,
			)

			require.Error(t, err)
			require.NotNil(t, result)
			require.True(t, result.PendingReconciliation)
			require.False(t, result.Retryable)
			require.Equal(t, service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, result.TerminalOutcome)
		})
	}
}

func TestOrganizationOpenAIChatAdapterClientDisconnectSettlesUsageWithoutRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard}
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, billing,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	adapter := &organizationChatCompletionsAdapter{
		handler:        &OpenAIGatewayHandler{gatewayService: gateway},
		c:              c,
		requestedModel: "gpt-5.4",
	}
	candidate := service.OrganizationChatCandidate{
		AccountID:      55,
		RequestedModel: "gpt-5.4",
		UpstreamModel:  "gpt-5.4",
		Route:          service.OrganizationChatRouteRawChat,
		Account:        &service.Account{ID: 55, Type: service.AccountTypeAPIKey},
		Pricing: service.OrganizationResponsesCandidatePricing{
			Model:             "gpt-5.4",
			RateMultiplier:    1,
			AccountMultiplier: 1,
			Pricing: &service.ModelPricing{
				InputPricePerToken:  0.000001,
				OutputPricePerToken: 0.000002,
			},
		},
	}

	result, err := adapter.resultForForward(
		service.OrganizationExecutionDispatchRequest{AttemptID: "attempt_disconnect", AttemptSequence: 1},
		candidate,
		&service.OpenAIForwardResult{
			Usage:            service.OpenAIUsage{InputTokens: 10, OutputTokens: 5},
			UsageObserved:    true,
			ClientDisconnect: true,
		},
		nil,
		0,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Dispatched)
	require.False(t, result.PendingReconciliation)
	require.Equal(t, service.OrganizationExecutionTerminalOutcomeError, result.TerminalOutcome)
	require.False(t, result.Retryable)
	require.NotNil(t, result.Observation)
	require.NotEmpty(t, result.Observation.RawActualCost)
	require.NotEqual(t, "0", result.Observation.RawActualCost)
	require.Equal(t, int64(10), result.Observation.InputTokens)
	require.Equal(t, int64(5), result.Observation.OutputTokens)
}

func organizationOpenAIChatRawAccount(id, groupID int64, priority int) service.Account {
	return service.Account{
		ID: id, Name: "raw chat", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 2, Priority: priority, GroupIDs: []int64{groupID},
		Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
		Extra: map[string]any{
			openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions),
		},
	}
}

func TestNonOpenAIOrganizationChatFailsClosedWithoutExecutionInjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-5.4","messages":[]}`)))
	apiKey := organizationResponsesAuthorizedAPIKey()
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.UserID})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	(&GatewayHandler{}).ChatCompletions(c)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "organization_billing_unavailable")
}

type organizationGatewayChatAccountRepoStub struct {
	service.AccountRepository
	accounts []service.Account
}

func (r *organizationGatewayChatAccountRepoStub) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, _ string) ([]service.Account, error) {
	return append([]service.Account(nil), r.accounts...), nil
}

func (r *organizationGatewayChatAccountRepoStub) GetByID(_ context.Context, accountID int64) (*service.Account, error) {
	for index := range r.accounts {
		if r.accounts[index].ID == accountID {
			account := r.accounts[index]
			return &account, nil
		}
	}
	return nil, nil
}

type organizationGatewayChatConcurrencyCacheStub struct {
	service.ConcurrencyCache
}

func (organizationGatewayChatConcurrencyCacheStub) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}

func (organizationGatewayChatConcurrencyCacheStub) ReleaseAccountSlot(context.Context, int64, string) error {
	return nil
}

func (organizationGatewayChatConcurrencyCacheStub) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}

func (organizationGatewayChatConcurrencyCacheStub) ReleaseUserSlot(context.Context, int64, string) error {
	return nil
}

type organizationGatewayChatQuotaRepoStub struct {
	service.UserPlatformQuotaRepository
}

func (organizationGatewayChatQuotaRepoStub) GetByUserPlatform(context.Context, int64, string) (*service.UserPlatformQuotaRecord, error) {
	return nil, nil
}

type organizationGatewayChatUserGroupRateRepoStub struct {
	service.UserGroupRateRepository
}

func (organizationGatewayChatUserGroupRateRepoStub) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	value := 0
	return &value, nil
}

type organizationGatewayChatRPMCacheStub struct{}

func (organizationGatewayChatRPMCacheStub) IncrementUserGroupRPM(context.Context, int64, int64) (int, error) {
	return 1, nil
}

func (organizationGatewayChatRPMCacheStub) IncrementUserRPM(context.Context, int64) (int, error) {
	return 1, nil
}

func (organizationGatewayChatRPMCacheStub) GetUserGroupRPM(context.Context, int64, int64) (int, error) {
	return 0, nil
}

func (organizationGatewayChatRPMCacheStub) GetUserRPM(context.Context, int64) (int, error) {
	return 0, nil
}

type organizationGatewayChatUpstreamStub struct {
	accountIDs []int64
}

type organizationGatewayChatRecoveryRepoStub struct {
	*organizationResponsesExecutionRepoStub
	pendingCommands []service.OrganizationPendingAttemptCommand
}

func (r *organizationGatewayChatRecoveryRepoStub) RecordPendingAttempt(_ context.Context, command service.OrganizationPendingAttemptCommand) error {
	r.pendingCommands = append(r.pendingCommands, command)
	return nil
}

func (*organizationGatewayChatRecoveryRepoStub) ReconcilePendingExecution(context.Context, service.OrganizationPendingReconciliationCommand) (*service.OrganizationFinalExecutionEvidenceResult, error) {
	return nil, nil
}

func (*organizationGatewayChatRecoveryRepoStub) ListPendingReconciliations(context.Context, int64, service.OrganizationPendingReconciliationListParams) ([]service.OrganizationPendingReconciliation, *service.OrganizationPendingReconciliationPage, error) {
	return nil, nil, nil
}

func (*organizationGatewayChatRecoveryRepoStub) GetPendingReconciliation(context.Context, int64, string) (*service.OrganizationPendingReconciliation, error) {
	return nil, nil
}

func (*organizationGatewayChatRecoveryRepoStub) RedriveAttemptAccountEffect(context.Context, string) (*service.OrganizationAttemptAccountEffectResult, error) {
	return nil, nil
}

func (*organizationGatewayChatRecoveryRepoStub) RedrivePendingAttemptAccountEffects(context.Context, int) ([]service.OrganizationAttemptAccountEffectResult, error) {
	return nil, nil
}

func (*organizationGatewayChatRecoveryRepoStub) RedriveExecutionOutbox(context.Context, int64) (*service.OrganizationExecutionOutboxApplyResult, error) {
	return nil, nil
}

func (u *organizationGatewayChatUpstreamStub) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return nil, nil
}

func (u *organizationGatewayChatUpstreamStub) DoWithTLS(_ *http.Request, _ string, accountID int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.accountIDs = append(u.accountIDs, accountID)
	if len(u.accountIDs) == 1 {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"temporary upstream failure"}}`)),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"anthropic-chat-2"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4","stop_reason":"","usage":{"input_tokens":12,"cache_read_input_tokens":3,"cache_creation_input_tokens":2}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}, nil
}

func TestOrganizationGatewayChatAnthropicStreamFailoverWithoutUsageIsPending(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 1
	enableOrganizationGatewayChatTestCapability(cfg)
	groupID := int64(42)
	organizationID := int64(88)
	role := service.OrganizationRoleMember
	accounts := []service.Account{
		organizationGatewayChatAnthropicAccount(55, groupID, 1),
		organizationGatewayChatAnthropicAccount(66, groupID, 2),
	}
	upstream := &organizationGatewayChatUpstreamStub{}
	concurrency := service.NewConcurrencyService(organizationGatewayChatConcurrencyCacheStub{})
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewGatewayService(
		&organizationGatewayChatAccountRepoStub{accounts: accounts}, nil, nil, nil, nil, nil, nil, nil, cfg, nil,
		concurrency, billing, nil, nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, organizationGatewayChatRPMCacheStub{}, organizationGatewayChatUserGroupRateRepoStub{}, cfg, organizationGatewayChatQuotaRepoStub{})
	t.Cleanup(billingCache.Stop)
	executionRepo := &organizationGatewayChatRecoveryRepoStub{organizationResponsesExecutionRepoStub: &organizationResponsesExecutionRepoStub{}}
	handler := NewGatewayHandler(gateway, nil, nil, nil, concurrency, billingCache, nil, nil, nil, nil, nil, nil, cfg, nil)
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))
	apiKey := &service.APIKey{
		ID: 9, UserID: 7, GroupID: &groupID, OrganizationID: &organizationID, Status: service.StatusActive,
		User:  &service.User{ID: 7, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &role, Concurrency: 2},
		Group: &service.Group{ID: groupID, Status: service.StatusActive, Hydrated: true, Platform: service.PlatformAnthropic, RateMultiplier: 1},
		OrganizationAuthorization: &service.OrganizationKeyAuthorization{
			APIKeyID: 9, UserID: 7, OrganizationID: organizationID, OrganizationStatus: service.OrganizationStatusActive, OrganizationBalanceVersion: 1,
			AllocationID: 77, AllocationUserID: 7, AllocationOrganizationID: organizationID, AllocationStatus: domain.OrganizationAllocationStatusActive, AllocationVersion: 1,
			Available: "10.00000000", Reserved: "0.00000000",
		},
	}
	body := []byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}],"stream":true,"max_tokens":64}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.UserID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.ChatCompletions(c)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Equal(t, []int64{55}, upstream.accountIDs)
	require.Equal(t, 1, executionRepo.reserveCalls)
	require.Empty(t, executionRepo.attemptCommands)
	require.Zero(t, executionRepo.terminalizeCalls)
	require.Len(t, executionRepo.pendingCommands, 1)
	require.Equal(t, int64(55), executionRepo.pendingCommands[0].AccountID)
	require.Equal(t, 1, executionRepo.pendingCommands[0].AttemptSequence)
	require.Contains(t, executionRepo.reserveCommand.CandidateSnapshotJSON, `"platform":"anthropic"`)
}

func TestOrganizationGatewayChatAdapterMarksPartialStreamPending(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	adapter := &organizationGatewayChatAdapter{c: c}
	result, err := adapter.resultForForward(
		service.OrganizationExecutionDispatchRequest{AttemptID: "attempt_partial", AttemptSequence: 1},
		service.OrganizationGatewayChatCandidate{AccountID: 55},
		&service.ForwardResult{Usage: service.ClaudeUsage{InputTokens: 8}},
		io.ErrUnexpectedEOF,
		0,
	)

	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.NotNil(t, result)
	require.True(t, result.Dispatched)
	require.True(t, result.PendingReconciliation)
	require.Equal(t, service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, result.TerminalOutcome)
}

func TestOrganizationGatewayChatClaudeCodeOnlyCreatesZeroCostEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	enableOrganizationGatewayChatTestCapability(cfg)
	apiKey := organizationGatewayChatOrganizationAPIKey()
	apiKey.Group.ClaudeCodeOnly = true
	executionRepo := &organizationResponsesExecutionRepoStub{}
	handler := &GatewayHandler{cfg: cfg, gatewayService: &service.GatewayService{}}
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}]}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.UserID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)

	handler.ChatCompletions(c)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Claude Code clients")
	require.Equal(t, "0.00000000", executionRepo.reserveCommand.CandidateRouteMaximumCost)
	require.Contains(t, executionRepo.reserveCommand.PreDispatchZeroProof, "organization_gateway_chat_claude_code_only")
}

func enableOrganizationGatewayChatTestCapability(cfg *config.Config) {
	cfg.Gateway.OrganizationSpending.Enabled = true
	cfg.Gateway.OrganizationSpending.Capabilities = map[string]map[string]bool{
		service.OrganizationEndpointFamilyChat: {
			service.OrganizationRequestShapeHTTP:      true,
			service.OrganizationRequestShapeStreaming: true,
		},
	}
}

func organizationGatewayChatAnthropicAccount(id, groupID int64, priority int) service.Account {
	return service.Account{
		ID: id, Name: "anthropic", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 2, Priority: priority, GroupIDs: []int64{groupID}, AccountGroups: []service.AccountGroup{{GroupID: groupID}},
		Credentials: map[string]any{"api_key": "test-upstream-key"},
	}
}

func organizationGatewayChatOrganizationAPIKey() *service.APIKey {
	organizationID := int64(88)
	groupID := int64(42)
	role := service.OrganizationRoleMember
	return &service.APIKey{
		ID: 9, UserID: 7, GroupID: &groupID, OrganizationID: &organizationID, Status: service.StatusActive,
		User:  &service.User{ID: 7, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &role, Concurrency: 2},
		Group: &service.Group{ID: groupID, Status: service.StatusActive, Hydrated: true, Platform: service.PlatformAnthropic, RateMultiplier: 1},
		OrganizationAuthorization: &service.OrganizationKeyAuthorization{
			APIKeyID: 9, UserID: 7, OrganizationID: organizationID, OrganizationStatus: service.OrganizationStatusActive, OrganizationBalanceVersion: 1,
			AllocationID: 77, AllocationUserID: 7, AllocationOrganizationID: organizationID, AllocationStatus: domain.OrganizationAllocationStatusActive, AllocationVersion: 1,
			Available: "10.00000000", Reserved: "0.00000000",
		},
	}
}
