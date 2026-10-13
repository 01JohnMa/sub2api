package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
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
	account  service.Account
	accounts []service.Account
}

func (r *organizationResponsesAccountRepoStub) GetByID(_ context.Context, accountID int64) (*service.Account, error) {
	for index := range r.accounts {
		if r.accounts[index].ID == accountID {
			copy := r.accounts[index]
			return &copy, nil
		}
	}
	copy := r.account
	return &copy, nil
}

func (r *organizationResponsesAccountRepoStub) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]service.Account, error) {
	if len(r.accounts) > 0 {
		return append([]service.Account(nil), r.accounts...), nil
	}
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

type organizationResponsesCountingConcurrencyCache struct {
	service.ConcurrencyCache
	mu              sync.Mutex
	userAcquires    int
	userReleases    int
	accountAcquires int
	accountReleases int
}

func (c *organizationResponsesCountingConcurrencyCache) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	c.mu.Lock()
	c.accountAcquires++
	c.mu.Unlock()
	return true, nil
}

func (c *organizationResponsesCountingConcurrencyCache) ReleaseAccountSlot(context.Context, int64, string) error {
	c.mu.Lock()
	c.accountReleases++
	c.mu.Unlock()
	return nil
}

func (c *organizationResponsesCountingConcurrencyCache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	c.mu.Lock()
	c.userAcquires++
	c.mu.Unlock()
	return true, nil
}

func (c *organizationResponsesCountingConcurrencyCache) ReleaseUserSlot(context.Context, int64, string) error {
	c.mu.Lock()
	c.userReleases++
	c.mu.Unlock()
	return nil
}

func (c *organizationResponsesCountingConcurrencyCache) counts() (userAcquires, userReleases, accountAcquires, accountReleases int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.userAcquires, c.userReleases, c.accountAcquires, c.accountReleases
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

type organizationResponsesCountingRPMCache struct {
	mu         sync.Mutex
	groupCalls int
	userCalls  int
}

func (c *organizationResponsesCountingRPMCache) IncrementUserGroupRPM(context.Context, int64, int64) (int, error) {
	c.mu.Lock()
	c.groupCalls++
	c.mu.Unlock()
	return 1, nil
}

func (c *organizationResponsesCountingRPMCache) IncrementUserRPM(context.Context, int64) (int, error) {
	c.mu.Lock()
	c.userCalls++
	c.mu.Unlock()
	return 1, nil
}

func (*organizationResponsesCountingRPMCache) GetUserGroupRPM(context.Context, int64, int64) (int, error) {
	return 0, nil
}

func (*organizationResponsesCountingRPMCache) GetUserRPM(context.Context, int64) (int, error) {
	return 0, nil
}

func (c *organizationResponsesCountingRPMCache) counts() (groupCalls, userCalls int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.groupCalls, c.userCalls
}

type organizationResponsesCountingAPIKeyRateRepo struct {
	service.APIKeyRepository
	mu    sync.Mutex
	calls int
}

func (r *organizationResponsesCountingAPIKeyRateRepo) GetRateLimitData(context.Context, int64) (*service.APIKeyRateLimitData, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	windowStart := time.Now().UTC()
	return &service.APIKeyRateLimitData{
		Window5hStart: &windowStart,
		Window1dStart: &windowStart,
		Window7dStart: &windowStart,
	}, nil
}

func (*organizationResponsesCountingAPIKeyRateRepo) ResetRateLimitWindows(context.Context, int64) error {
	return nil
}

func (r *organizationResponsesCountingAPIKeyRateRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

type organizationResponsesUserGroupRateRepoStub struct {
	service.UserGroupRateRepository
}

func (organizationResponsesUserGroupRateRepoStub) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	value := 0
	return &value, nil
}

type organizationResponsesExecutionRepoStub struct {
	reserveCommand   service.OrganizationExecutionReservationCommand
	reserveCalls     int
	attemptCommand   service.OrganizationAttemptEvidenceCommand
	attemptCommands  []service.OrganizationAttemptEvidenceCommand
	terminalCommand  service.OrganizationTerminalizationCommand
	terminalizeCalls int
}

func (r *organizationResponsesExecutionRepoStub) Reserve(_ context.Context, command service.OrganizationExecutionReservationCommand) (*service.OrganizationExecutionReservationResult, error) {
	r.reserveCalls++
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
	r.attemptCommands = append(r.attemptCommands, command)
	return &service.OrganizationAttemptEvidenceResult{AttemptEvidenceID: int64(201 + len(r.attemptCommands)), ExecutionID: command.ExecutionID, AttemptID: command.AttemptID}, nil
}

func (r *organizationResponsesExecutionRepoStub) Terminalize(_ context.Context, command service.OrganizationTerminalizationCommand) (*service.OrganizationFinalExecutionEvidenceResult, error) {
	r.terminalizeCalls++
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

func TestOrganizationResponsesFrozenChatFallbackUsesOneFinalOrganizationDebit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 1
	enableOrganizationResponsesTestCapability(cfg)

	groupID := int64(42)
	accounts := []service.Account{
		{
			ID: 55, Name: "native", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 2, Priority: 1, GroupIDs: []int64{groupID},
			Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
			Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)},
		},
		{
			ID: 66, Name: "chat fallback", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 2, Priority: 2, GroupIDs: []int64{groupID},
			Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
			Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions)},
		},
	}
	upstream := &organizationResponsesFailoverUpstreamStub{}
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

	body := []byte(`{"model":"gpt-5.4","input":"hello","max_output_tokens":64}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	apiKey := organizationResponsesAuthorizedAPIKey()
	c.Set(string(middleware2.ContextKeyOrganizationKeyAuthorization), apiKey.OrganizationAuthorization)
	setOpenAIClientTransportHTTP(c)
	streamStarted := false

	handler.handleOrganizationResponses(c, apiKey, middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: apiKey.User.Concurrency}, nil, body, "gpt-5.4", false, body, zap.NewNop(), &streamStarted)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []int64{55, 66}, upstream.accountIDs)
	require.Equal(t, 1, executionRepo.reserveCalls)
	require.Len(t, executionRepo.attemptCommands, 2)
	require.Equal(t, int64(55), executionRepo.attemptCommands[0].AccountID)
	require.Equal(t, int64(66), executionRepo.attemptCommands[1].AccountID)
	require.Equal(t, service.OrganizationExecutionTerminalOutcomeSuccess, executionRepo.terminalCommand.TerminalOutcome)
	require.Equal(t, 1, executionRepo.terminalizeCalls)
	require.NotNil(t, executionRepo.terminalCommand.FinalAttemptID)
	require.Equal(t, executionRepo.attemptCommands[1].AttemptID, *executionRepo.terminalCommand.FinalAttemptID)
	require.NotEqual(t, executionRepo.attemptCommands[0].AttemptID, *executionRepo.terminalCommand.FinalAttemptID)
	require.True(t, gjson.Get(executionRepo.reserveCommand.CandidateSnapshotJSON, "candidates.1.route").Exists())
	require.Equal(t, service.OrganizationResponsesRouteChatFallback, gjson.Get(executionRepo.reserveCommand.CandidateSnapshotJSON, "candidates.1.route").String())
	require.Equal(t, "0", executionRepo.attemptCommands[0].RawActualCost)
	require.NotEqual(t, "0", executionRepo.attemptCommands[1].RawActualCost)
}

type organizationResponsesFailoverUpstreamStub struct {
	accountIDs []int64
}

func (u *organizationResponsesFailoverUpstreamStub) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
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
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"req_chat_fallback"}},
		Body: io.NopCloser(bytes.NewBufferString(
			`{"id":"chatcmpl_1","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`,
		)),
	}, nil
}

func (u *organizationResponsesFailoverUpstreamStub) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
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

func TestOrganizationResponsesWebSocketTurnRequiresCurrentAttribution(t *testing.T) {
	apiKey := organizationResponsesAuthorizedAPIKey()
	require.True(t, isCurrentOrganizationResponsesWSKey(apiKey))

	apiKey.OrganizationAuthorization = nil
	require.False(t, isCurrentOrganizationResponsesWSKey(apiKey), "member removal clears the trusted organization attribution before the next turn")
}

func TestOrganizationResponsesWebSocketPlanningPayloadUsesSessionModelOnlyWhenOmitted(t *testing.T) {
	payload, err := organizationWSPlanningPayload([]byte(`{"type":"response.create","input":"hello"}`), "gpt-5.4")
	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", gjson.GetBytes(payload, "model").String())

	_, err = organizationWSPlanningPayload([]byte(`{"type":"response.create","input":"hello"}`), "")
	require.ErrorIs(t, err, service.ErrOrganizationResponsesUnsupportedRequest)
}

func TestOrganizationResponsesWebSocketCoordinatorRemovesTerminalTurn(t *testing.T) {
	state := &organizationResponsesWSTurn{
		readyDone: make(chan struct{}), done: make(chan struct{}),
	}
	close(state.done)
	coordinator := &organizationResponsesWSTurnCoordinator{turns: map[int]*organizationResponsesWSTurn{2: state}}

	coordinator.AfterTurn(2, &service.OpenAIForwardResult{UsageObserved: true}, nil)

	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	require.Empty(t, coordinator.turns)
}

func TestOrganizationResponsesWebSocketCoordinatorSnapshotsRequestMetadata(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/backend-api/codex/responses", nil)
	c.Request.Header.Set("X-Request-ID", "request-before")

	coordinator := newOrganizationResponsesWSTurnCoordinator(nil, c, "secret", nil, zap.NewNop())
	c.Request.URL.Path = "/mutated"
	c.Request.Header.Set("X-Request-ID", "request-after")

	require.Equal(t, "/backend-api/codex/responses", coordinator.requestPath)
	require.Equal(t, "request-before", coordinator.clientRequestID)
}

func TestOrganizationResponsesWebSocketTurnReadySignalIsIdempotent(t *testing.T) {
	state := &organizationResponsesWSTurn{readyDone: make(chan struct{})}
	state.signalReady(nil)
	state.signalReady(errors.New("late persistence failure"))

	<-state.readyDone
	require.NoError(t, state.readyErr)
}

func TestOrganizationResponsesWebSocketRetryPreservesLogicalTurnAndWaitsForNewAccount(t *testing.T) {
	firstAccount := &service.Account{ID: 55}
	secondAccount := &service.Account{ID: 66}
	state := &organizationResponsesWSTurn{
		done:          make(chan struct{}),
		payload:       []byte(`{"type":"response.create","model":"gpt-5.4","input":"retry me"}`),
		originalModel: "gpt-5.4",
		lastAccountID: firstAccount.ID,
	}
	coordinator := &organizationResponsesWSTurnCoordinator{
		activeAccount:  firstAccount,
		turns:          map[int]*organizationResponsesWSTurn{4: state},
		accountChanged: make(chan struct{}),
		retryRequests:  make(chan organizationResponsesWSRetryRequest, 1),
		turnFinished:   make(chan int, 1),
	}

	coordinator.requestRetry(4, state, secondAccount.ID)
	retry, err := coordinator.WaitRetry(context.Background())
	require.NoError(t, err)
	require.Equal(t, 4, retry.turn)
	require.JSONEq(t, string(state.payload), string(retry.payload))
	require.Equal(t, state.originalModel, retry.originalModel)
	require.Equal(t, secondAccount.ID, retry.nextAccountID)

	type activeAccountResult struct {
		account *service.Account
		err     error
	}
	accountResult := make(chan activeAccountResult, 1)
	go func() {
		account, waitErr := coordinator.waitForActiveAccount(context.Background(), firstAccount.ID)
		accountResult <- activeAccountResult{account: account, err: waitErr}
	}()
	coordinator.SetActiveAccount(secondAccount)
	active := <-accountResult
	require.NoError(t, active.err)
	require.Equal(t, secondAccount.ID, active.account.ID)

	state.signalRetryReady(nil)
	require.NoError(t, <-retry.ready)
}

func TestOrganizationResponsesWebSocketAccountHandoffReleasesSchedulerLeaseBeforeWake(t *testing.T) {
	slot := make(chan struct{}, 1)
	slot <- struct{}{}
	releaseCalls := 0
	currentRelease := func() {
		<-slot
		releaseCalls++
	}
	releaseAccountSlot := func() {
		if currentRelease != nil {
			currentRelease()
			currentRelease = nil
		}
	}
	firstAccount := &service.Account{ID: 55}
	secondAccount := &service.Account{ID: 66}
	coordinator := &organizationResponsesWSTurnCoordinator{
		activeAccount: firstAccount, accountChanged: make(chan struct{}),
	}
	attemptAcquired := make(chan bool, 1)
	go func() {
		account, err := coordinator.waitForActiveAccount(context.Background(), firstAccount.ID)
		if err != nil || account.ID != secondAccount.ID {
			attemptAcquired <- false
			return
		}
		select {
		case slot <- struct{}{}:
			attemptAcquired <- true
		default:
			attemptAcquired <- false
		}
	}()

	handoffOrganizationResponsesWSAccount(releaseAccountSlot, coordinator, secondAccount)
	require.True(t, <-attemptAcquired, "attempt must only wake after the scheduler-held account slot is released")
	releaseAccountSlot()
	require.Equal(t, 1, releaseCalls, "deferred cleanup must not release the scheduler lease twice")
}

func TestOrganizationResponsesWebSocketRetryRejectsSimultaneousPendingTurns(t *testing.T) {
	coordinator := &organizationResponsesWSTurnCoordinator{
		turns: map[int]*organizationResponsesWSTurn{
			1: {done: make(chan struct{})},
			2: {done: make(chan struct{})},
		},
		retryRequests: make(chan organizationResponsesWSRetryRequest, 1),
		turnFinished:  make(chan int, 1),
	}

	_, err := coordinator.WaitRetry(context.Background())
	require.ErrorIs(t, err, service.ErrOrganizationResponsesWSFailoverUnsupported)
}

func TestOrganizationResponsesWebSocketFreezesActiveAccountFirstThenRetriesInOrder(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	groupID := int64(42)
	newAccount := func(id int64) service.Account {
		return service.Account{
			ID: id, Name: "account", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 2, GroupIDs: []int64{groupID},
			Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
			Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)},
		}
	}
	defaultFirst := newAccount(55)
	activeFirst := newAccount(66)
	accountRepo := &organizationResponsesAccountRepoStub{accounts: []service.Account{defaultFirst, activeFirst}}
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewOpenAIGatewayService(
		accountRepo, nil, nil, nil, nil, nil, nil, cfg, nil, nil, billing,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	apiKey := organizationResponsesAuthorizedAPIKey()
	apiKey.GroupID = &groupID
	apiKey.Group.ID = groupID
	body := []byte(`{"type":"response.create","model":"gpt-5.4","input":"hello","max_output_tokens":32}`)
	plan, err := gateway.PlanOrganizationResponsesCandidates(context.Background(), service.OrganizationResponsesCandidatePlanInput{
		APIKey: apiKey, Body: body, RequestShape: service.OrganizationRequestShapeWebSocketResponseCreate,
		RequestPath: "/v1/responses", GroupID: &groupID, MaximumAttempts: 2, PreferredAccountID: activeFirst.ID,
	})
	require.NoError(t, err)
	require.Len(t, plan.Candidates, 2)
	require.Equal(t, []int64{activeFirst.ID, defaultFirst.ID}, []int64{plan.Candidates[0].AccountID, plan.Candidates[1].AccountID})

	handler := NewOpenAIGatewayHandler(gateway, nil, nil, nil, nil, nil, nil, cfg)
	coordinator := &organizationResponsesWSTurnCoordinator{
		handler: handler, activeAccount: &activeFirst, accountChanged: make(chan struct{}),
	}
	state := &organizationResponsesWSTurn{}
	adapter := &organizationResponsesWSTurnAdapter{coordinator: coordinator, state: state, apiKey: apiKey, plan: plan}

	first, err := adapter.currentCandidate(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, activeFirst.ID, first.AccountID)

	coordinator.SetActiveAccount(&defaultFirst)
	second, err := adapter.currentCandidate(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, defaultFirst.ID, second.AccountID)
}

func TestOrganizationResponsesWebSocketParentAdmissionRunsOnceAcrossTwoAttempts(t *testing.T) {
	cfg := &config.Config{RunMode: config.RunModeStandard, Log: config.LogConfig{Environment: "development"}}
	cfg.Default.RateMultiplier = 1
	groupID := int64(42)
	newAccount := func(id int64) service.Account {
		return service.Account{
			ID: id, Name: "account", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID},
			Credentials: map[string]any{"api_key": "test-upstream-key", "base_url": "https://api.openai.com"},
			Extra:       map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses)},
		}
	}
	firstAccount := newAccount(55)
	secondAccount := newAccount(66)
	accountRepo := &organizationResponsesAccountRepoStub{accounts: []service.Account{firstAccount, secondAccount}}
	concurrencyCache := &organizationResponsesCountingConcurrencyCache{}
	concurrency := service.NewConcurrencyService(concurrencyCache)
	rpmCache := &organizationResponsesCountingRPMCache{}
	rateRepo := &organizationResponsesCountingAPIKeyRateRepo{}
	billingCache := service.NewBillingCacheService(
		nil, nil, nil, rateRepo, rpmCache, organizationResponsesUserGroupRateRepoStub{}, cfg,
		organizationResponsesUserPlatformQuotaRepoStub{},
	)
	defer billingCache.Stop()
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewOpenAIGatewayService(
		accountRepo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency, billing,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	apiKey := organizationResponsesAuthorizedAPIKey()
	apiKey.GroupID = &groupID
	apiKey.Group.ID = groupID
	apiKey.User.RPMLimit = 10
	apiKey.RateLimit5h = 100
	groupOverride := 10
	apiKey.User.UserGroupRPMOverride = &groupOverride
	body := []byte(`{"type":"response.create","model":"gpt-5.4","input":"hello","max_output_tokens":32}`)
	plan, err := gateway.PlanOrganizationResponsesCandidates(context.Background(), service.OrganizationResponsesCandidatePlanInput{
		APIKey: apiKey, Body: body, RequestShape: service.OrganizationRequestShapeWebSocketResponseCreate,
		RequestPath: "/v1/responses", GroupID: &groupID, MaximumAttempts: 2, PreferredAccountID: firstAccount.ID,
	})
	require.NoError(t, err)
	require.Len(t, plan.Candidates, 2)

	handler := NewOpenAIGatewayHandler(gateway, concurrency, billingCache, nil, nil, nil, nil, cfg)
	state := &organizationResponsesWSTurn{readyDone: make(chan struct{})}
	coordinator := &organizationResponsesWSTurnCoordinator{
		handler: handler, activeAccount: &firstAccount, accountChanged: make(chan struct{}),
		retryRequests: make(chan organizationResponsesWSRetryRequest, 1),
	}
	adapter := &organizationResponsesWSTurnAdapter{coordinator: coordinator, turn: 1, state: state, apiKey: apiKey, plan: plan}
	defer adapter.releaseParentAdmission()

	type dispatchResult struct {
		result *service.OrganizationExecutionAttemptResult
		err    error
	}
	firstResult := make(chan dispatchResult, 1)
	go func() {
		result, dispatchErr := adapter.Dispatch(context.Background(), service.OrganizationExecutionDispatchRequest{AttemptSequence: 1})
		firstResult <- dispatchResult{result: result, err: dispatchErr}
	}()
	<-state.readyDone
	state.completeAttempt(organizationResponsesWSTurnOutcome{err: &service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}})
	first := <-firstResult
	require.NoError(t, first.err)
	require.True(t, first.result.Retryable)

	retry := <-coordinator.retryRequests
	coordinator.SetActiveAccount(&secondAccount)
	secondResult := make(chan dispatchResult, 1)
	go func() {
		result, dispatchErr := adapter.Dispatch(context.Background(), service.OrganizationExecutionDispatchRequest{AttemptSequence: 2})
		secondResult <- dispatchResult{result: result, err: dispatchErr}
	}()
	require.NoError(t, <-retry.ready)
	state.completeAttempt(organizationResponsesWSTurnOutcome{err: &service.OpenAILocalPreDispatchError{
		Message: "known pre-dispatch failure", ZeroProof: service.OpenAIWSUpstreamPreDispatchZeroProof,
	}})
	second := <-secondResult
	require.NoError(t, second.err)
	require.False(t, second.result.Dispatched)
	require.False(t, second.result.PendingReconciliation)

	adapter.releaseParentAdmission()
	groupRPMCalls, userRPMCalls := rpmCache.counts()
	userAcquires, userReleases, accountAcquires, accountReleases := concurrencyCache.counts()
	require.Equal(t, 1, groupRPMCalls)
	require.Equal(t, 1, userRPMCalls)
	require.Equal(t, 1, rateRepo.count())
	require.Equal(t, 1, userAcquires)
	require.Equal(t, 1, userReleases)
	require.Equal(t, 2, accountAcquires)
	require.Equal(t, 2, accountReleases)
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

	t.Run("websocket upstream pre-dispatch failure is proven zero", func(t *testing.T) {
		adapter, request := newAdapter()
		result, err := adapter.resultForForward(request, candidate, nil, &service.OpenAILocalPreDispatchError{
			Message: "handshake failed", ZeroProof: service.OpenAIWSUpstreamPreDispatchZeroProof,
		}, -1)
		require.NoError(t, err)
		require.False(t, result.Dispatched)
		require.False(t, result.PendingReconciliation)
		require.Equal(t, service.OpenAIWSUpstreamPreDispatchZeroProof, result.ZeroProof)
	})
}
