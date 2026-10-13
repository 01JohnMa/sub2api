package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type organizationGeminiPendingRepoStub struct {
	*organizationResponsesExecutionRepoStub
	pending service.OrganizationPendingAttemptCommand
}

type organizationGeminiPreparedUpstreamStub struct {
	bodies [][]byte
}

func (s *organizationGeminiPreparedUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	s.bodies = append(s.bodies, body)
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"response":{"error":{"code":404,"message":"model not found","status":"NOT_FOUND"}}}`)),
	}, nil
}

func (s *organizationGeminiPreparedUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, concurrency)
}

func (r *organizationGeminiPendingRepoStub) RecordPendingAttempt(_ context.Context, c service.OrganizationPendingAttemptCommand) error {
	r.pending = c
	return nil
}
func (r *organizationGeminiPendingRepoStub) ReconcilePendingExecution(context.Context, service.OrganizationPendingReconciliationCommand) (*service.OrganizationFinalExecutionEvidenceResult, error) {
	return nil, errors.New("unexpected reconcile")
}
func (r *organizationGeminiPendingRepoStub) ListPendingReconciliations(context.Context, int64, service.OrganizationPendingReconciliationListParams) ([]service.OrganizationPendingReconciliation, *service.OrganizationPendingReconciliationPage, error) {
	return nil, nil, errors.New("unexpected list")
}
func (r *organizationGeminiPendingRepoStub) GetPendingReconciliation(context.Context, int64, string) (*service.OrganizationPendingReconciliation, error) {
	return nil, errors.New("unexpected get")
}
func (r *organizationGeminiPendingRepoStub) RedriveAttemptAccountEffect(context.Context, string) (*service.OrganizationAttemptAccountEffectResult, error) {
	return nil, nil
}
func (r *organizationGeminiPendingRepoStub) RedrivePendingAttemptAccountEffects(context.Context, int) ([]service.OrganizationAttemptAccountEffectResult, error) {
	return nil, nil
}
func (r *organizationGeminiPendingRepoStub) RedriveExecutionOutbox(context.Context, int64) (*service.OrganizationExecutionOutboxApplyResult, error) {
	return nil, nil
}

func TestGeminiV1BetaOrganizationKeyFailsClosedWithoutExecutionService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3.1-pro:generateContent", http.NoBody)
	c.Request.Body = io.NopCloser(strings.NewReader(`{"contents":[{"parts":[{"text":"hello"}]}]}`))
	c.Params = gin.Params{{Key: "modelAction", Value: "gemini-3.1-pro:generateContent"}}
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID: 1, Group: &service.Group{Platform: service.PlatformGemini}, User: &service.User{ID: 2},
	})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 2})
	c.Set(string(middleware.ContextKeyOrganizationKeyAuthorization), &service.OrganizationKeyAuthorization{})

	(&GatewayHandler{}).GeminiV1BetaModels(c)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Organization billing")
}

func TestAntigravityAliasOrganizationKeyFailsClosedWithoutExecutionService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/antigravity/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`))
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1, User: &service.User{ID: 2}})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 2})
	c.Set(string(middleware.ContextKeyOrganizationKeyAuthorization), &service.OrganizationKeyAuthorization{})
	c.Set(string(middleware.ContextKeyForcePlatform), service.PlatformAntigravity)

	(&GatewayHandler{}).Messages(c)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "organization billing dependencies are unavailable")
}

func TestOrganizationGeminiV1BetaEarlyRejectionsPersistZeroCostEvidenceForNativeAndAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name     string
		path     string
		platform string
		family   string
	}{
		{name: "native", path: "/v1beta/models/gemini-3.1-pro:generateContent", platform: service.PlatformGemini, family: service.OrganizationEndpointFamilyGemini},
		{name: "antigravity alias", path: "/antigravity/v1beta/models/gemini-3.1-pro:generateContent", platform: service.PlatformAntigravity, family: service.OrganizationEndpointFamilyAntigravity},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo := &organizationResponsesExecutionRepoStub{}
			handler := &GatewayHandler{organizationExecution: service.NewOrganizationExecutionService(repo)}
			apiKey := organizationResponsesAuthorizedAPIKey()
			apiKey.Group.Platform = testCase.platform

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, testCase.path, http.NoBody)
			c.Params = gin.Params{{Key: "modelAction", Value: "gemini-3.1-pro:generateContent"}}
			c.Set(string(middleware.ContextKeyAPIKey), apiKey)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID})
			c.Set(string(middleware.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)
			if testCase.platform == service.PlatformAntigravity {
				c.Set(string(middleware.ContextKeyForcePlatform), service.PlatformAntigravity)
			}

			handler.GeminiV1BetaModels(c)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Equal(t, testCase.family, repo.reserveCommand.EndpointFamily)
			require.Equal(t, "0.00000000", repo.reserveCommand.CandidateRouteMaximumCost)
			require.Contains(t, repo.reserveCommand.PreDispatchZeroProof, "organization_gemini_unsupported_request")
		})
	}
}

func TestOrganizationGeminiAdapterExecutesFrozenCandidatesOnce(t *testing.T) {
	apiKey := organizationResponsesAuthorizedAPIKey()
	repo := &organizationResponsesExecutionRepoStub{}
	execution := service.NewOrganizationExecutionService(repo)
	first := service.OrganizationGeminiCandidate{AccountID: 11, RequestedModel: "gemini-3.1-pro", DispatchModel: "gemini-3.1-pro", UpstreamModel: "gemini-3.1-pro", Route: service.OrganizationGeminiRoute{AccountID: 11, Platform: service.PlatformGemini}}
	second := service.OrganizationGeminiCandidate{AccountID: 12, RequestedModel: "gemini-3.1-pro", DispatchModel: "gemini-3.1-pro", UpstreamModel: "gemini-3.1-pro", ForwardBody: []byte(`{"frozen":true}`), PreparedPayload: true, Route: service.OrganizationGeminiRoute{AccountID: 12, Platform: service.PlatformGemini}}
	plan := &service.OrganizationGeminiCandidatePlan{Policy: service.OrganizationGeminiRequestPolicy{EndpointFamily: service.OrganizationEndpointFamilyGemini, RequestShape: service.OrganizationRequestShapeModelsAction, Action: "generateContent", RequestedModel: "gemini-3.1-pro"}, Candidates: []service.OrganizationGeminiCandidate{first, second}, ForwardBody: []byte(`{"contents":[]}`), CandidateSnapshot: []byte(`{"candidates":[]}`), PricingSnapshot: []byte(`{"candidates":[]}`), CandidateRouteMaximumCost: "1.00000000", BillingFingerprint: strings.Repeat("a", 64), MaximumAttempts: 2}
	input, err := plan.NewExecutionReservationInput(apiKey, "")
	require.NoError(t, err)
	command, err := execution.NewReservationCommand(input)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3.1-pro:generateContent", nil)
	adapter := &organizationGeminiV1BetaAdapter{handler: &GatewayHandler{gatewayService: &service.GatewayService{}}, c: c, apiKey: apiKey, plan: plan, body: plan.ForwardBody, admissionDone: true}
	adapter.revalidate = func(_ context.Context, _ *service.APIKey, candidate service.OrganizationGeminiCandidate, _ service.OrganizationGeminiRequestPolicy, _ []byte) (*service.OrganizationGeminiCandidate, error) {
		candidate.Account = &service.Account{ID: candidate.AccountID, Concurrency: 1}
		return &candidate, nil
	}
	adapter.acquireAccountSlotTest = func(context.Context, service.OrganizationGeminiCandidate) (func(), bool, error) {
		return nil, true, nil
	}
	adapter.actualCostTest = func(service.OrganizationGeminiCandidate, *service.ForwardResult) (string, service.UsageTokens, error) {
		return "0.50000000", service.UsageTokens{InputTokens: 1}, nil
	}
	adapter.forwardTest = func(_ context.Context, candidate service.OrganizationGeminiCandidate) (*service.ForwardResult, error) {
		if candidate.AccountID == 11 {
			return nil, &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway}
		}
		return &service.ForwardResult{RequestID: "second", Usage: service.ClaudeUsage{InputTokens: 1}}, nil
	}
	run, err := execution.ExecuteMultiAttempt(context.Background(), *command, 2, adapter)
	require.NoError(t, err)
	require.Len(t, run.Attempts, 2)
	require.Equal(t, 1, repo.reserveCalls)
	require.Len(t, repo.attemptCommands, 2)
	require.Equal(t, int64(12), repo.attemptCommands[1].AccountID)
	require.Equal(t, repo.attemptCommands[1].AttemptID, *repo.terminalCommand.FinalAttemptID)
}

func TestOrganizationGeminiAdapterStreamDisconnectRemainsPending(t *testing.T) {
	apiKey := organizationResponsesAuthorizedAPIKey()
	repo := &organizationGeminiPendingRepoStub{organizationResponsesExecutionRepoStub: &organizationResponsesExecutionRepoStub{}}
	execution := service.NewOrganizationExecutionService(repo)
	candidate := service.OrganizationGeminiCandidate{AccountID: 12, RequestedModel: "gemini-3.1-pro", DispatchModel: "gemini-3.1-pro", UpstreamModel: "gemini-3.1-pro", ForwardBody: []byte(`{"frozen":true}`), PreparedPayload: true, Route: service.OrganizationGeminiRoute{AccountID: 12, Platform: service.PlatformGemini}}
	plan := &service.OrganizationGeminiCandidatePlan{Policy: service.OrganizationGeminiRequestPolicy{EndpointFamily: service.OrganizationEndpointFamilyGemini, RequestShape: service.OrganizationRequestShapeModelsAction, Action: "streamGenerateContent", Stream: true, RequestedModel: "gemini-3.1-pro"}, Candidates: []service.OrganizationGeminiCandidate{candidate}, ForwardBody: []byte(`{"contents":[]}`), CandidateSnapshot: []byte(`{"candidates":[]}`), PricingSnapshot: []byte(`{"candidates":[]}`), CandidateRouteMaximumCost: "1.00000000", BillingFingerprint: strings.Repeat("a", 64), MaximumAttempts: 1}
	input, err := plan.NewExecutionReservationInput(apiKey, "")
	require.NoError(t, err)
	command, err := execution.NewReservationCommand(input)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3.1-pro:streamGenerateContent", nil)
	adapter := &organizationGeminiV1BetaAdapter{handler: &GatewayHandler{gatewayService: &service.GatewayService{}}, c: c, apiKey: apiKey, plan: plan, body: plan.ForwardBody, admissionDone: true}
	adapter.revalidate = func(_ context.Context, _ *service.APIKey, v service.OrganizationGeminiCandidate, _ service.OrganizationGeminiRequestPolicy, _ []byte) (*service.OrganizationGeminiCandidate, error) {
		v.Account = &service.Account{ID: v.AccountID, Concurrency: 1}
		return &v, nil
	}
	adapter.acquireAccountSlotTest = func(context.Context, service.OrganizationGeminiCandidate) (func(), bool, error) {
		return nil, true, nil
	}
	adapter.forwardTest = func(context.Context, service.OrganizationGeminiCandidate) (*service.ForwardResult, error) {
		return &service.ForwardResult{RequestID: "disconnect", ClientDisconnect: true}, nil
	}
	adapter.actualCostTest = func(service.OrganizationGeminiCandidate, *service.ForwardResult) (string, service.UsageTokens, error) {
		return "", service.UsageTokens{}, service.ErrOrganizationExecutionPendingReconciliation
	}
	run, err := execution.ExecuteMultiAttempt(context.Background(), *command, 1, adapter)
	require.NoError(t, err)
	require.True(t, run.PendingReconciliation)
	require.Nil(t, run.Final)
	require.Equal(t, 0, repo.terminalizeCalls)
	require.NotEmpty(t, repo.pending.ProviderRequestID)
	require.NotEmpty(t, repo.pending.ProviderCorrelationID)
}

func TestOrganizationGeminiAdapterForwardsPreparedAntigravityPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(42)
	account := &service.Account{
		ID: 55, Name: "antigravity", Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{
			"project_id": "project-test", "access_token": "test-token",
			"model_mapping": map[string]any{
				"gemini-3.1-pro":    "gemini-3.1-pro",
				"claude-sonnet-4-5": "claude-sonnet-4-5",
			},
		},
	}
	prepareService := service.NewAntigravityGatewayService(nil, nil, nil, nil, nil, nil, nil, nil)
	geminiBody := []byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`)
	preparedGemini, err := prepareService.PrepareOrganizationGeminiPayload(account, "gemini-3.1-pro", "generateContent", geminiBody)
	require.NoError(t, err)
	messagesBody := []byte(`{"model":"claude-sonnet-4-5","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)
	preparedMessages, err := prepareService.PrepareOrganizationMessagesPayload(context.Background(), account, messagesBody)
	require.NoError(t, err)

	for _, testCase := range []struct {
		name         string
		plan         service.OrganizationGeminiCandidatePlan
		candidate    service.OrganizationGeminiCandidate
		adapterBody  []byte
		expectedBody []byte
	}{
		{
			name: "Gemini mixed Antigravity candidate",
			plan: service.OrganizationGeminiCandidatePlan{Policy: service.OrganizationGeminiRequestPolicy{
				EndpointFamily: service.OrganizationEndpointFamilyGemini, Action: "generateContent", RequestedModel: "gemini-3.1-pro",
			}},
			candidate: service.OrganizationGeminiCandidate{
				AccountID: account.ID, Account: account, RequestedModel: "gemini-3.1-pro", DispatchModel: "gemini-3.1-pro", UpstreamModel: preparedGemini.DispatchModel,
				ForwardBody: preparedGemini.Payload, PreparedPayload: true,
			},
			adapterBody: geminiBody, expectedBody: preparedGemini.Payload,
		},
		{
			name: "Antigravity messages candidate",
			plan: service.OrganizationGeminiCandidatePlan{Policy: service.OrganizationGeminiRequestPolicy{
				EndpointFamily: service.OrganizationEndpointFamilyAntigravity, Action: "messages", RequestedModel: "claude-sonnet-4-5",
			}},
			candidate: service.OrganizationGeminiCandidate{
				AccountID: account.ID, Account: account, RequestedModel: "claude-sonnet-4-5", DispatchModel: "claude-sonnet-4-5", UpstreamModel: preparedMessages.DispatchModel,
				ForwardBody: preparedMessages.Payload, PreparedPayload: true,
			},
			adapterBody: messagesBody, expectedBody: preparedMessages.Payload,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			upstream := &organizationGeminiPreparedUpstreamStub{}
			antigravityGateway := service.NewAntigravityGatewayService(nil, nil, nil, &service.AntigravityTokenProvider{}, nil, upstream, nil, nil)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/test:generateContent", bytes.NewReader(testCase.adapterBody))
			adapter := &organizationGeminiV1BetaAdapter{
				handler: &GatewayHandler{gatewayService: &service.GatewayService{}, antigravityGatewayService: antigravityGateway},
				c:       c, apiKey: &service.APIKey{GroupID: &groupID}, plan: &testCase.plan, body: testCase.adapterBody,
			}

			_, err := adapter.forward(context.Background(), testCase.candidate)

			require.Error(t, err)
			require.Len(t, upstream.bodies, 1)
			require.Equal(t, testCase.expectedBody, upstream.bodies[0])
		})
	}
}
