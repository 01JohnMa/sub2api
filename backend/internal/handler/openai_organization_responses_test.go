package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type organizationResponsesAccountRepoStub struct {
	service.AccountRepository
	account service.Account
}

func (r *organizationResponsesAccountRepoStub) GetByID(context.Context, int64) (*service.Account, error) {
	copy := r.account
	return &copy, nil
}

func (r *organizationResponsesAccountRepoStub) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]service.Account, error) {
	return []service.Account{r.account}, nil
}

type organizationResponsesHTTPUpstreamStub struct {
	request *http.Request
	body    []byte
}

func (u *organizationResponsesHTTPUpstreamStub) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.request = req.Clone(req.Context())
	u.body, _ = io.ReadAll(req.Body)
	body := `{"id":"resp_org","object":"response","model":"gpt-5.4","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2,"input_tokens_details":{"cached_tokens":0}}}`
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"x-request-id": []string{"req_upstream"},
		},
		Body: io.NopCloser(bytes.NewBufferString(body)),
	}, nil
}

func (u *organizationResponsesHTTPUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

type organizationResponsesConcurrencyCacheStub struct {
	service.ConcurrencyCache
}

func (organizationResponsesConcurrencyCacheStub) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}

func (organizationResponsesConcurrencyCacheStub) ReleaseAccountSlot(context.Context, int64, string) error {
	return nil
}

func (organizationResponsesConcurrencyCacheStub) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}

func (organizationResponsesConcurrencyCacheStub) ReleaseUserSlot(context.Context, int64, string) error {
	return nil
}

type organizationResponsesUserPlatformQuotaRepoStub struct {
	service.UserPlatformQuotaRepository
}

func (organizationResponsesUserPlatformQuotaRepoStub) GetByUserPlatform(context.Context, int64, string) (*service.UserPlatformQuotaRecord, error) {
	return nil, nil
}

type organizationResponsesUserRPMCacheStub struct{}

func (organizationResponsesUserRPMCacheStub) IncrementUserGroupRPM(context.Context, int64, int64) (int, error) {
	return 1, nil
}

func (organizationResponsesUserRPMCacheStub) IncrementUserRPM(context.Context, int64) (int, error) {
	return 1, nil
}

func (organizationResponsesUserRPMCacheStub) GetUserGroupRPM(context.Context, int64, int64) (int, error) {
	return 0, nil
}

func (organizationResponsesUserRPMCacheStub) GetUserRPM(context.Context, int64) (int, error) {
	return 0, nil
}

type organizationResponsesUserGroupRateRepoStub struct {
	service.UserGroupRateRepository
}

func (organizationResponsesUserGroupRateRepoStub) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	value := 0
	return &value, nil
}

type organizationResponsesExecutionRepoStub struct {
	reserveCommand  service.OrganizationExecutionReservationCommand
	attemptCommand  service.OrganizationAttemptEvidenceCommand
	terminalCommand service.OrganizationTerminalizationCommand
}

func (r *organizationResponsesExecutionRepoStub) Reserve(_ context.Context, command service.OrganizationExecutionReservationCommand) (*service.OrganizationExecutionReservationResult, error) {
	r.reserveCommand = command
	if command.PreDispatchZeroProof != "" {
		occurredAt := time.Now().UTC()
		return &service.OrganizationExecutionReservationResult{
			ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
			OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID,
			APIKeyID: command.Attribution.APIKeyID, AllocationID: command.Attribution.AllocationID,
			Disposition: service.OrganizationExecutionReservationDispositionPreDispatchRejected,
			FinalEvidence: &service.OrganizationFinalExecutionEvidenceResult{
				FinalEvidenceID: 303, ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
				OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID,
				APIKeyID: command.Attribution.APIKeyID, AllocationID: command.Attribution.AllocationID,
				TerminalOutcome: service.OrganizationExecutionTerminalOutcomeError,
				RawActualCost:   "0", OrganizationSettledCost: "0.00000000",
				ZeroProof: command.PreDispatchZeroProof, OccurredAt: occurredAt,
			},
		}, nil
	}
	return &service.OrganizationExecutionReservationResult{
		ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
		OrganizationID: command.Attribution.OrganizationID, UserID: command.Attribution.UserID,
		APIKeyID: command.Attribution.APIKeyID, AllocationID: command.Attribution.AllocationID,
		ReservationID: 101, ReservedAmount: command.CandidateRouteMaximumCost,
		State: service.OrganizationReservationStateReserved, Disposition: service.OrganizationExecutionReservationDispositionReserved,
	}, nil
}

func TestOrganizationResponsesPlanRejectionCreatesZeroCostFinalEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	enableOrganizationResponsesTestCapability(cfg)
	account := service.Account{
		ID: 55, Name: "native", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{42},
		Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)},
	}
	accountRepo := &organizationResponsesAccountRepoStub{account: account}
	upstream := &organizationResponsesHTTPUpstreamStub{}
	concurrency := service.NewConcurrencyService(organizationResponsesConcurrencyCacheStub{})
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewOpenAIGatewayService(
		accountRepo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, billing,
		nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil,
	)
	executionRepo := &organizationResponsesExecutionRepoStub{}
	handler := NewOpenAIGatewayHandler(gateway, concurrency, nil, nil, nil, nil, nil, cfg)
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))

	organizationID := int64(88)
	organizationRole := service.OrganizationRoleMember
	groupID := int64(42)
	apiKey := &service.APIKey{
		ID: 9, UserID: 7, GroupID: &groupID, OrganizationID: &organizationID, Status: service.StatusActive,
		User:  &service.User{ID: 7, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &organizationRole, Concurrency: 2},
		Group: &service.Group{ID: groupID, Status: service.StatusActive, Hydrated: true, Platform: service.PlatformOpenAI, RateMultiplier: 1},
		OrganizationAuthorization: &service.OrganizationKeyAuthorization{
			APIKeyID: 9, UserID: 7, OrganizationID: organizationID, OrganizationStatus: service.OrganizationStatusActive,
			OrganizationBalanceVersion: 1, AllocationID: 77, AllocationUserID: 7, AllocationOrganizationID: organizationID,
			AllocationStatus: domain.OrganizationAllocationStatusActive, AllocationVersion: 1, Available: "10.00000000", Reserved: "0.00000000",
		},
	}
	for _, testCase := range []struct {
		name string
		path string
		body []byte
	}{
		{name: "unsupported tool", path: "/v1/responses", body: []byte(`{"model":"gpt-5.4","input":"hello","tools":[{"type":"web_search"}]}`)},
		{name: "unsupported compact subpath", path: "/v1/responses/compact", body: []byte(`{"model":"gpt-5.4","input":"hello","max_output_tokens":64}`)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			upstream.request = nil
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, testCase.path, bytes.NewReader(testCase.body))
			c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)
			setOpenAIClientTransportHTTP(c)
			streamStarted := false

			handler.handleOrganizationResponses(c, apiKey, middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency}, nil, testCase.body, "gpt-5.4", false, testCase.body, zap.NewNop(), &streamStarted)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Nil(t, upstream.request)
			require.Equal(t, "0.00000000", executionRepo.reserveCommand.CandidateRouteMaximumCost)
			require.Equal(t, "request_policy_rejected:organization_responses_unsupported_request", executionRepo.reserveCommand.PreDispatchZeroProof)
		})
	}
}

func TestOrganizationResponsesCapabilityRejectionCreatesZeroCostFinalEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "production"}}
	cfg.Default.RateMultiplier = 1
	enableOrganizationResponsesTestCapability(cfg)
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, billing,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	executionRepo := &organizationResponsesExecutionRepoStub{}
	handler := NewOpenAIGatewayHandler(gateway, nil, nil, nil, nil, nil, nil, cfg)
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))

	organizationID := int64(88)
	organizationRole := service.OrganizationRoleMember
	groupID := int64(42)
	apiKey := &service.APIKey{
		ID: 9, UserID: 7, GroupID: &groupID, OrganizationID: &organizationID, Status: service.StatusActive,
		User:  &service.User{ID: 7, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &organizationRole, Concurrency: 2},
		Group: &service.Group{ID: groupID, Status: service.StatusActive, Hydrated: true, Platform: service.PlatformOpenAI, RateMultiplier: 1},
		OrganizationAuthorization: &service.OrganizationKeyAuthorization{
			APIKeyID: 9, UserID: 7, OrganizationID: organizationID, OrganizationStatus: service.OrganizationStatusActive,
			OrganizationBalanceVersion: 1, AllocationID: 77, AllocationUserID: 7, AllocationOrganizationID: organizationID,
			AllocationStatus: domain.OrganizationAllocationStatusActive, AllocationVersion: 1, Available: "10.00000000", Reserved: "0.00000000",
		},
	}
	body := []byte(`{"model":"gpt-5.4","input":"hello","max_output_tokens":64}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)
	setOpenAIClientTransportHTTP(c)
	streamStarted := false

	handler.handleOrganizationResponses(c, apiKey, middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency}, nil, body, "gpt-5.4", false, body, zap.NewNop(), &streamStarted)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "ORGANIZATION_SPENDING_DISABLED")
	require.Equal(t, "0.00000000", executionRepo.reserveCommand.CandidateRouteMaximumCost)
	require.Equal(t, "request_policy_rejected:organization_spending_disabled", executionRepo.reserveCommand.PreDispatchZeroProof)
}

func TestOrganizationResponsesEarlyRequestValidationCreatesZeroCostFinalEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	enableOrganizationResponsesTestCapability(cfg)
	billing := service.NewBillingService(cfg, nil)
	account := service.Account{
		ID: 55, Name: "native", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{42},
		Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)},
	}
	concurrency := service.NewConcurrencyService(organizationResponsesConcurrencyCacheStub{})
	gateway := service.NewOpenAIGatewayService(
		&organizationResponsesAccountRepoStub{account: account}, nil, nil, nil, nil, nil, nil, cfg, nil,
		concurrency, billing,
		nil, nil, &organizationResponsesHTTPUpstreamStub{}, nil, nil, nil, nil, nil, nil, nil,
	)
	executionRepo := &organizationResponsesExecutionRepoStub{}
	handler := NewOpenAIGatewayHandler(gateway, concurrency, nil, nil, nil, nil, nil, cfg)
	handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))
	apiKey := organizationResponsesAuthorizedAPIKey()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":`))
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency})
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)
	setOpenAIClientTransportHTTP(c)

	handler.Responses(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Empty(t, executionRepo.attemptCommand.ProviderRequestID)
	require.Equal(t, "0.00000000", executionRepo.reserveCommand.CandidateRouteMaximumCost)
	require.Equal(t, "request_policy_rejected:organization_responses_unsupported_request", executionRepo.reserveCommand.PreDispatchZeroProof)
}

func (r *organizationResponsesExecutionRepoStub) RecordAttemptEvidence(_ context.Context, command service.OrganizationAttemptEvidenceCommand) (*service.OrganizationAttemptEvidenceResult, error) {
	r.attemptCommand = command
	return &service.OrganizationAttemptEvidenceResult{AttemptEvidenceID: 202, ExecutionID: command.ExecutionID, AttemptID: command.AttemptID}, nil
}

func (r *organizationResponsesExecutionRepoStub) Terminalize(_ context.Context, command service.OrganizationTerminalizationCommand) (*service.OrganizationFinalExecutionEvidenceResult, error) {
	r.terminalCommand = command
	rawCost := "0"
	settledCost := "0.00000000"
	reservationState := service.OrganizationReservationStateReleased
	var consumptionPostingID *int64
	if command.FinalAttemptID != nil {
		rawCost = r.attemptCommand.RawActualCost
		cost, err := decimal.NewFromString(rawCost)
		if err != nil {
			return nil, err
		}
		settledCost = cost.Round(8).StringFixed(8)
		if !cost.IsZero() {
			reservationState = service.OrganizationReservationStateSettled
			id := int64(303)
			consumptionPostingID = &id
		}
	}
	return &service.OrganizationFinalExecutionEvidenceResult{
		FinalEvidenceID: 404, ExecutionID: command.ExecutionID, CorrelationID: command.CorrelationID,
		OrganizationID: command.OrganizationID, UserID: command.UserID, APIKeyID: command.APIKeyID,
		AllocationID: command.AllocationID, ReservationID: command.ReservationID,
		FinalAttemptID: command.FinalAttemptID, FinalAttemptEvidenceID: command.FinalAttemptEvidenceID,
		TerminalOutcome: command.TerminalOutcome, BusinessLimited: command.BusinessLimited,
		RawActualCost: rawCost, OrganizationSettledCost: settledCost, ZeroProof: command.ZeroProof,
		ReservationState: reservationState, ConsumptionPostingID: consumptionPostingID, OccurredAt: command.OccurredAt,
	}, nil
}

func TestOrganizationResponsesHTTPAliasesShareExecutionSnapshotsAndProviderCorrelation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.4","input":"hello","max_output_tokens":64}`)
	var candidateSnapshot string
	var pricingSnapshot string

	for index, path := range []string{"/v1/responses", "/responses", "/backend-api/codex/responses"} {
		t.Run(path, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
			cfg.Default.RateMultiplier = 1
			cfg.Gateway.MaxAccountSwitches = 1
			enableOrganizationResponsesTestCapability(cfg)
			account := service.Account{
				ID: 55, Name: "native", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
				Status: service.StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{42},
				Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
				Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)},
			}
			accountRepo := &organizationResponsesAccountRepoStub{account: account}
			upstream := &organizationResponsesHTTPUpstreamStub{}
			concurrency := service.NewConcurrencyService(organizationResponsesConcurrencyCacheStub{})
			billing := service.NewBillingService(cfg, nil)
			gateway := service.NewOpenAIGatewayService(
				accountRepo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, billing,
				nil, nil, upstream, nil, nil, nil, nil, nil, nil, nil,
			)
			executionRepo := &organizationResponsesExecutionRepoStub{}
			billingCache := service.NewBillingCacheService(
				nil, nil, nil, nil,
				organizationResponsesUserRPMCacheStub{}, organizationResponsesUserGroupRateRepoStub{}, cfg,
				organizationResponsesUserPlatformQuotaRepoStub{},
			)
			t.Cleanup(billingCache.Stop)
			handler := NewOpenAIGatewayHandler(gateway, concurrency, billingCache, nil, nil, nil, nil, cfg)
			handler.SetOrganizationExecutionService(service.NewOrganizationExecutionService(executionRepo))

			organizationID := int64(88)
			organizationRole := service.OrganizationRoleMember
			groupID := int64(42)
			apiKey := &service.APIKey{
				ID: 9, UserID: 7, GroupID: &groupID, OrganizationID: &organizationID, Status: service.StatusActive,
				User:  &service.User{ID: 7, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &organizationRole, Concurrency: 2},
				Group: &service.Group{ID: groupID, Status: service.StatusActive, Hydrated: true, Platform: service.PlatformOpenAI, RateMultiplier: 1},
				OrganizationAuthorization: &service.OrganizationKeyAuthorization{
					APIKeyID: 9, UserID: 7, OrganizationID: organizationID, OrganizationStatus: service.OrganizationStatusActive,
					OrganizationBalanceVersion: 1, AllocationID: 77, AllocationUserID: 7, AllocationOrganizationID: organizationID,
					AllocationStatus: domain.OrganizationAllocationStatusActive, AllocationVersion: 1, Available: "10.00000000", Reserved: "0.00000000",
				},
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)
			setOpenAIClientTransportHTTP(c)
			streamStarted := false

			handler.handleOrganizationResponses(c, apiKey, middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency}, nil, body, "gpt-5.4", false, body, zap.NewNop(), &streamStarted)

			require.Equal(t, http.StatusOK, recorder.Code)
			require.NotNil(t, upstream.request)
			require.Equal(t, executionRepo.attemptCommand.ProviderRequestID, upstream.request.Header.Get("X-Client-Request-Id"))
			require.EqualValues(t, 64, gjson.GetBytes(upstream.body, "max_output_tokens").Int())
			require.Equal(t, "req_upstream", recorder.Header().Get("x-request-id"))
			require.Equal(t, service.OrganizationExecutionTerminalOutcomeSuccess, executionRepo.terminalCommand.TerminalOutcome)
			require.NotNil(t, executionRepo.terminalCommand.FinalAttemptID)
			if index == 0 {
				candidateSnapshot = executionRepo.reserveCommand.CandidateSnapshotJSON
				pricingSnapshot = executionRepo.reserveCommand.PricingSnapshotJSON
			} else {
				require.Equal(t, candidateSnapshot, executionRepo.reserveCommand.CandidateSnapshotJSON)
				require.Equal(t, pricingSnapshot, executionRepo.reserveCommand.PricingSnapshotJSON)
			}
		})
	}
}

func enableOrganizationResponsesTestCapability(cfg *config.Config) {
	cfg.Gateway.OrganizationSpending.Enabled = true
	cfg.Gateway.OrganizationSpending.Capabilities = map[string]map[string]bool{
		service.OrganizationEndpointFamilyResponses: {
			service.OrganizationRequestShapeHTTP:      true,
			service.OrganizationRequestShapeStreaming: true,
		},
	}
}

func organizationResponsesAuthorizedAPIKey() *service.APIKey {
	organizationID := int64(88)
	organizationRole := service.OrganizationRoleMember
	groupID := int64(42)
	return &service.APIKey{
		ID: 9, UserID: 7, GroupID: &groupID, OrganizationID: &organizationID, Status: service.StatusActive,
		User:  &service.User{ID: 7, Status: service.StatusActive, OrganizationID: &organizationID, OrganizationRole: &organizationRole, Concurrency: 2},
		Group: &service.Group{ID: groupID, Status: service.StatusActive, Hydrated: true, Platform: service.PlatformOpenAI, RateMultiplier: 1},
		OrganizationAuthorization: &service.OrganizationKeyAuthorization{
			APIKeyID: 9, UserID: 7, OrganizationID: organizationID, OrganizationStatus: service.OrganizationStatusActive,
			OrganizationBalanceVersion: 1, AllocationID: 77, AllocationUserID: 7, AllocationOrganizationID: organizationID,
			AllocationStatus: domain.OrganizationAllocationStatusActive, AllocationVersion: 1, Available: "10.00000000", Reserved: "0.00000000",
		},
	}
}

func TestOrganizationResponsesAdapterClassifiesTerminalOutcomes(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	billing := service.NewBillingService(cfg, nil)
	policy, err := service.BuildOrganizationResponsesRequestPolicy(
		[]byte(`{"model":"gpt-5.4","input":"hello","max_output_tokens":64}`),
		service.OrganizationRequestShapeHTTP,
		"/v1/responses",
	)
	require.NoError(t, err)
	pricing, err := service.BuildOrganizationResponsesCandidatePricing(billing, nil, policy, "gpt-5.4", 1)
	require.NoError(t, err)
	account := &service.Account{ID: 55, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Concurrency: 2}
	candidate := service.OrganizationResponsesCandidate{
		AccountID: 55, AccountType: service.AccountTypeAPIKey, RequestedModel: "gpt-5.4",
		UpstreamModel: "gpt-5.4", Route: "openai_http_responses", Pricing: pricing, Account: account,
	}
	gateway := service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, billing,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	newAdapter := func() (*organizationResponsesHTTPAdapter, service.OrganizationExecutionDispatchRequest) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		handler := &OpenAIGatewayHandler{gatewayService: gateway}
		return &organizationResponsesHTTPAdapter{handler: handler, c: c, requestedModel: "gpt-5.4", reqLog: zap.NewNop()}, service.OrganizationExecutionDispatchRequest{
			ProviderRequestID: "2d4dcd3f-76ab-4a04-b73d-bcf8f7b5ec12", ProviderCorrelationID: "70599853-5800-44ca-998c-9f4dd495ea73",
		}
	}

	t.Run("client disconnect settles observed usage as terminal error", func(t *testing.T) {
		adapter, request := newAdapter()
		result, err := adapter.resultForForward(request, candidate, &service.OpenAIForwardResult{
			RequestID: "req_disconnect", Usage: service.OpenAIUsage{InputTokens: 10, OutputTokens: 2}, UsageObserved: true, ClientDisconnect: true,
		}, nil, -1)
		require.NoError(t, err)
		require.True(t, result.Dispatched)
		require.Equal(t, service.OrganizationExecutionTerminalOutcomeError, result.TerminalOutcome)
		require.NotEqual(t, "0", result.Observation.RawActualCost)
		require.False(t, result.PendingReconciliation)
	})

	t.Run("missing usage remains pending reconciliation", func(t *testing.T) {
		adapter, request := newAdapter()
		result, err := adapter.resultForForward(request, candidate, &service.OpenAIForwardResult{RequestID: "req_missing_usage"}, nil, -1)
		require.ErrorIs(t, err, service.ErrOrganizationExecutionPendingReconciliation)
		require.True(t, result.Dispatched)
		require.True(t, result.PendingReconciliation)
		require.Equal(t, service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, result.TerminalOutcome)
	})

	t.Run("known failover is retryable attempt evidence", func(t *testing.T) {
		adapter, request := newAdapter()
		result, err := adapter.resultForForward(request, candidate, nil, &service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}, -1)
		require.NoError(t, err)
		require.True(t, result.Dispatched)
		require.True(t, result.Retryable)
		require.Equal(t, "0", result.Observation.RawActualCost)
		require.False(t, result.PendingReconciliation)
	})

	t.Run("transport failover without an HTTP response remains pending", func(t *testing.T) {
		adapter, request := newAdapter()
		result, err := adapter.resultForForward(request, candidate, nil, &service.UpstreamFailoverError{
			StatusCode: http.StatusBadGateway, NoHTTPResponse: true,
		}, -1)
		require.Error(t, err)
		require.True(t, result.PendingReconciliation)
		require.Equal(t, service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, result.TerminalOutcome)
	})

	t.Run("timeout without usage remains pending reconciliation", func(t *testing.T) {
		adapter, request := newAdapter()
		result, err := adapter.resultForForward(request, candidate, nil, context.DeadlineExceeded, -1)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.True(t, result.Dispatched)
		require.True(t, result.PendingReconciliation)
		require.Equal(t, service.OrganizationAttemptEvidenceTerminalOutcomeUnknown, result.TerminalOutcome)
	})

	t.Run("local policy rejection is proven zero", func(t *testing.T) {
		adapter, request := newAdapter()
		result, err := adapter.resultForForward(request, candidate, nil, &service.BetaBlockedError{Message: "blocked before dispatch"}, -1)
		require.NoError(t, err)
		require.False(t, result.Dispatched)
		require.Equal(t, "local_policy_rejected", result.ZeroProof)
	})

	t.Run("typed fast policy rejection is proven zero", func(t *testing.T) {
		adapter, request := newAdapter()
		result, err := adapter.resultForForward(request, candidate, nil, &service.OpenAIFastBlockedError{Message: "blocked before dispatch"}, -1)
		require.NoError(t, err)
		require.False(t, result.Dispatched)
		require.Equal(t, "local_policy_rejected", result.ZeroProof)
	})

	t.Run("upstream error text cannot prove zero", func(t *testing.T) {
		adapter, request := newAdapter()
		upstreamErr := errors.New("upstream error: service_tier priority is not allowed")
		result, err := adapter.resultForForward(request, candidate, nil, upstreamErr, -1)
		require.ErrorIs(t, err, upstreamErr)
		require.True(t, result.Dispatched)
		require.True(t, result.PendingReconciliation)
	})

	t.Run("generic post-dispatch error is never released blindly", func(t *testing.T) {
		adapter, request := newAdapter()
		result, err := adapter.resultForForward(request, candidate, nil, errors.New("transport outcome unknown"), -1)
		require.EqualError(t, err, "transport outcome unknown")
		require.True(t, result.PendingReconciliation)
	})

	t.Run("local token acquisition failure is proven zero", func(t *testing.T) {
		adapter, request := newAdapter()
		result, err := adapter.resultForForward(request, candidate, nil, &service.OpenAILocalPreDispatchError{Message: "token unavailable"}, -1)
		require.NoError(t, err)
		require.False(t, result.Dispatched)
		require.Equal(t, "local_policy_rejected", result.ZeroProof)
	})
}
