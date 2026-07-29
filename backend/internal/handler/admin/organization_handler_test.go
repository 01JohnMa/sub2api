package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type organizationManagerStub struct {
	items        []service.Organization
	organization *service.Organization
	createActor  int64
	createInput  service.CreateOrganizationInput
	updateID     int64
	updateInput  service.UpdateOrganizationInput
	listParams   pagination.PaginationParams
	listFilters  service.OrganizationListFilters
	err          error
}

type organizationMembershipManagerStub struct {
	items        []service.OrganizationMember
	member       *service.OrganizationMember
	listOrgID    int64
	addOrgID     int64
	addUserID    int64
	addRole      string
	changeOrgID  int64
	changeUserID int64
	changeRole   string
	removeActor  service.OrganizationMemberRemovalActor
	removeOrgID  int64
	removeUserID int64
	removeInput  service.OrganizationMemberRemovalInput
	removeKey    string
	removeResult *service.OrganizationMemberRemovalResult
	err          error
}

type organizationFundingRepositoryStub struct {
	creditInput  service.OrganizationCreditCommand
	creditResult *service.OrganizationFundingResult
	summaryOrgID int64
	summary      *service.OrganizationBalanceSummary
	ledgerOrgID  int64
	ledgerParams pagination.PaginationParams
	ledger       []service.OrganizationLedgerEntry
	ledgerPage   *pagination.PaginationResult
	err          error
}

type organizationAllocationManagerStub struct {
	previewActor service.OrganizationAllocationActor
	previewOrgID int64
	previewOp    string
	previewInput service.OrganizationAllocationPreviewInput
	preview      *service.OrganizationAllocationPreview
	executeActor service.OrganizationAllocationActor
	executeOrgID int64
	executeOp    string
	executeInput service.OrganizationAllocationExecuteInput
	executeKey   string
	result       *service.OrganizationAllocationResult
}

func (s *organizationAllocationManagerStub) Preview(_ context.Context, actor service.OrganizationAllocationActor, organizationID int64, operationType string, input service.OrganizationAllocationPreviewInput) (*service.OrganizationAllocationPreview, error) {
	s.previewActor, s.previewOrgID, s.previewOp, s.previewInput = actor, organizationID, operationType, input
	return s.preview, nil
}

func (s *organizationAllocationManagerStub) Execute(_ context.Context, actor service.OrganizationAllocationActor, organizationID int64, operationType string, input service.OrganizationAllocationExecuteInput, idempotencyKey string) (*service.OrganizationAllocationResult, error) {
	s.executeActor, s.executeOrgID, s.executeOp, s.executeInput, s.executeKey = actor, organizationID, operationType, input, idempotencyKey
	return s.result, nil
}

func (s *organizationFundingRepositoryStub) Credit(_ context.Context, input service.OrganizationCreditCommand) (*service.OrganizationFundingResult, error) {
	s.creditInput = input
	if s.err != nil {
		return nil, s.err
	}
	if s.creditResult != nil {
		return s.creditResult, nil
	}
	before := "10.00000000"
	after := "11.25000000"
	return &service.OrganizationFundingResult{
		OperationID:          input.OperationID,
		CorrelationID:        input.CorrelationID,
		OrganizationID:       input.OrganizationID,
		Currency:             input.Currency,
		Amount:               input.Amount,
		PoolBefore:           before,
		PoolAfter:            after,
		BalanceVersionBefore: 3,
		BalanceVersionAfter:  4,
		CanonicalPayloadHash: input.CanonicalPayloadHash,
		Ledger: service.OrganizationLedgerEntry{
			PostingID:            91,
			PostingSequence:      1,
			OperationID:          input.OperationID,
			CorrelationID:        input.CorrelationID,
			OrganizationID:       input.OrganizationID,
			OperationType:        input.OperationType,
			AuthorizationScope:   input.AuthorizationScope,
			ActorIDSnapshot:      input.Actor.UserID,
			ActorDisplaySnapshot: input.Actor.DisplayName,
			ActorRoleSnapshot:    input.Actor.PlatformRole,
			Currency:             input.Currency,
			IdempotencyKeyHash:   input.IdempotencyKeyHash,
			CanonicalPayloadHash: input.CanonicalPayloadHash,
			PublicReason:         input.PublicReason,
			InternalReason:       input.InternalReason,
			BucketType:           domain.OrganizationLedgerBucketUnallocated,
			BucketIDSnapshot:     input.OrganizationID,
			BeforeAmount:         before,
			DeltaAmount:          input.Amount,
			AfterAmount:          after,
			BalanceVersionBefore: 3,
			BalanceVersionAfter:  4,
			OccurredAt:           input.OccurredAt,
			CreatedAt:            input.OccurredAt,
		},
	}, nil
}

func (s *organizationFundingRepositoryStub) GetSummary(_ context.Context, organizationID int64) (*service.OrganizationBalanceSummary, error) {
	s.summaryOrgID = organizationID
	return s.summary, s.err
}

func (s *organizationFundingRepositoryStub) ListLedger(_ context.Context, organizationID int64, params pagination.PaginationParams) ([]service.OrganizationLedgerEntry, *pagination.PaginationResult, error) {
	s.ledgerOrgID = organizationID
	s.ledgerParams = params
	return s.ledger, s.ledgerPage, s.err
}

func (s *organizationMembershipManagerStub) List(_ context.Context, organizationID int64, _ pagination.PaginationParams) ([]service.OrganizationMember, *pagination.PaginationResult, error) {
	s.listOrgID = organizationID
	return s.items, &pagination.PaginationResult{Total: int64(len(s.items)), Page: 1, PageSize: 20, Pages: 1}, s.err
}

func (s *organizationMembershipManagerStub) Add(_ context.Context, organizationID, userID int64, role string) (*service.OrganizationMember, error) {
	s.addOrgID = organizationID
	s.addUserID = userID
	s.addRole = role
	return s.member, s.err
}

func (s *organizationMembershipManagerStub) ChangeRole(_ context.Context, organizationID, userID int64, role string) (*service.OrganizationMember, error) {
	s.changeOrgID = organizationID
	s.changeUserID = userID
	s.changeRole = role
	return s.member, s.err
}

func (s *organizationMembershipManagerStub) Remove(_ context.Context, actor service.OrganizationMemberRemovalActor, organizationID, targetUserID int64, input service.OrganizationMemberRemovalInput, idempotencyKey string) (*service.OrganizationMemberRemovalResult, error) {
	s.removeActor = actor
	s.removeOrgID = organizationID
	s.removeUserID = targetUserID
	s.removeInput = input
	s.removeKey = idempotencyKey
	return s.removeResult, s.err
}

func (s *organizationManagerStub) Create(_ context.Context, actorID int64, input service.CreateOrganizationInput) (*service.Organization, error) {
	s.createActor = actorID
	s.createInput = input
	return s.organization, s.err
}

func (s *organizationManagerStub) Get(context.Context, int64) (*service.Organization, error) {
	return s.organization, s.err
}

func (s *organizationManagerStub) List(_ context.Context, params pagination.PaginationParams, filters service.OrganizationListFilters) ([]service.Organization, *pagination.PaginationResult, error) {
	s.listParams = params
	s.listFilters = filters
	return s.items, &pagination.PaginationResult{Total: int64(len(s.items)), Page: 1, PageSize: 20, Pages: 1}, s.err
}

func (s *organizationManagerStub) Update(_ context.Context, id int64, input service.UpdateOrganizationInput) (*service.Organization, error) {
	s.updateID = id
	s.updateInput = input
	return s.organization, s.err
}

func newOrganizationHandlerRouter(handler *OrganizationHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	withSubject := func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Set(string(middleware.ContextKeyUserRole), service.RoleAdmin)
		c.Set("auth_method", "jwt")
		c.Next()
	}
	router.GET("/organizations", handler.List)
	router.GET("/organizations/:id", handler.Get)
	router.POST("/organizations", withSubject, handler.Create)
	router.PUT("/organizations/:id", handler.Update)
	router.GET("/organizations/:id/members", handler.ListMembers)
	router.POST("/organizations/:id/members", handler.AddMember)
	router.PUT("/organizations/:id/members/:user_id", handler.UpdateMember)
	router.DELETE("/organizations/:id/members/:user_id", withSubject, handler.RemoveMember)
	router.POST("/organizations/:id/credit", withSubject, handler.Credit)
	router.GET("/organizations/:id/balance", handler.GetBalance)
	router.GET("/organizations/:id/ledger", handler.ListLedger)
	router.POST("/organizations/:id/allocations/custom/preview", withSubject, handler.PreviewCustomAllocation)
	router.POST("/organizations/:id/allocations/custom", withSubject, handler.ExecuteCustomAllocation)
	router.POST("/organizations/:id/allocations/reclaim/preview", withSubject, handler.PreviewReclaimAllocation)
	router.POST("/organizations/:id/allocations/reclaim", withSubject, handler.ExecuteReclaimAllocation)
	return router
}

func TestOrganizationHandlerCreateUsesAuthenticatedPlatformActor(t *testing.T) {
	organization := &service.Organization{ID: 9, Name: "Example", Code: "example", Status: service.OrganizationStatusActive, CreatedBy: 42, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	stub := &organizationManagerStub{organization: organization}
	router := newOrganizationHandlerRouter(NewOrganizationHandler(stub, &organizationMembershipManagerStub{}, nil, nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/organizations", strings.NewReader(`{"name":"Example","code":"EXAMPLE","description":"Test"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, int64(42), stub.createActor)
	require.Equal(t, "EXAMPLE", stub.createInput.Code)
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Equal(t, 0, envelope.Code)
	require.Equal(t, int64(9), envelope.Data.ID)
}

func TestOrganizationHandlerUpdateRejectsInvalidID(t *testing.T) {
	stub := &organizationManagerStub{}
	router := newOrganizationHandlerRouter(NewOrganizationHandler(stub, &organizationMembershipManagerStub{}, nil, nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/organizations/not-an-id", strings.NewReader(`{"name":"Example"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Zero(t, stub.updateID)
}

func TestOrganizationHandlerListUsesPaginatedEnvelope(t *testing.T) {
	stub := &organizationManagerStub{items: []service.Organization{{ID: 1, Name: "Example", Code: "example", Status: service.OrganizationStatusActive}}}
	router := newOrganizationHandlerRouter(NewOrganizationHandler(stub, &organizationMembershipManagerStub{}, nil, nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/organizations?status=active&search=example&sort_by=code&sort_order=asc", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var envelope struct {
		Data struct {
			Items []struct {
				Code string `json:"code"`
			} `json:"items"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	require.Equal(t, int64(1), envelope.Data.Total)
	require.Equal(t, "example", envelope.Data.Items[0].Code)
	require.Equal(t, "active", stub.listFilters.Status)
	require.Equal(t, "example", stub.listFilters.Search)
	require.Equal(t, "code", stub.listParams.SortBy)
	require.Equal(t, "asc", stub.listParams.SortOrder)
}

func TestOrganizationHandlerAddMemberUsesOrganizationScopedPayload(t *testing.T) {
	membership := &organizationMembershipManagerStub{member: &service.OrganizationMember{
		UserID: 7, Email: "member@example.com", OrganizationRole: service.OrganizationRoleAdmin,
		Allocation: service.OrganizationMemberAllocation{ID: 11, Status: "active", Available: "0.00000000", Reserved: "0.00000000"},
	}}
	router := newOrganizationHandlerRouter(NewOrganizationHandler(&organizationManagerStub{}, membership, nil, nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/organizations/3/members", strings.NewReader(`{"user_id":7,"organization_role":"organization_admin"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, int64(3), membership.addOrgID)
	require.Equal(t, int64(7), membership.addUserID)
	require.Equal(t, service.OrganizationRoleAdmin, membership.addRole)
	require.Contains(t, rec.Body.String(), `"available":"0.00000000"`)
}

func TestOrganizationHandlerUpdateMemberUsesPathScopedUser(t *testing.T) {
	membership := &organizationMembershipManagerStub{member: &service.OrganizationMember{UserID: 7, OrganizationRole: service.OrganizationRoleMember}}
	router := newOrganizationHandlerRouter(NewOrganizationHandler(&organizationManagerStub{}, membership, nil, nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/organizations/3/members/7", strings.NewReader(`{"organization_role":"organization_member"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(3), membership.changeOrgID)
	require.Equal(t, int64(7), membership.changeUserID)
	require.Equal(t, service.OrganizationRoleMember, membership.changeRole)
}

func TestOrganizationHandlerListMembersUsesPaginatedEnvelope(t *testing.T) {
	membership := &organizationMembershipManagerStub{items: []service.OrganizationMember{{UserID: 7, Email: "member@example.com"}}}
	router := newOrganizationHandlerRouter(NewOrganizationHandler(&organizationManagerStub{}, membership, nil, nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/organizations/3/members", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(3), membership.listOrgID)
	require.Contains(t, rec.Body.String(), `"items"`)
	require.Contains(t, rec.Body.String(), `"user_id":7`)
}

func TestOrganizationHandlerCreditRequiresInteractiveJWTAndPreservesDecimalStrings(t *testing.T) {
	fundingRepo := &organizationFundingRepositoryStub{}
	handler := NewOrganizationHandler(
		&organizationManagerStub{},
		&organizationMembershipManagerStub{},
		service.NewOrganizationFundingService(fundingRepo),
		nil,
	)
	router := newOrganizationHandlerRouter(handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/organizations/7/credit", strings.NewReader(`{
		"amount":"1.25000000",
		"public_reason":"Quarterly allowance",
		"internal_reason":"approved by finance"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "credit-2026-q3")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(7), fundingRepo.creditInput.OrganizationID)
	require.Equal(t, "1.25000000", fundingRepo.creditInput.Amount)
	require.Equal(t, int64(42), fundingRepo.creditInput.Actor.UserID)
	require.Equal(t, domain.OrganizationFundingAuthorizationPlatformAdminJWT, fundingRepo.creditInput.AuthorizationScope)
	require.NotEmpty(t, fundingRepo.creditInput.IdempotencyKeyHash)
	require.Contains(t, rec.Body.String(), `"pool_before":"10.00000000"`)
	require.Contains(t, rec.Body.String(), `"pool_after":"11.25000000"`)
}

func TestOrganizationHandlerCreditRejectsAdminAPIKeyAuthentication(t *testing.T) {
	fundingRepo := &organizationFundingRepositoryStub{}
	handler := NewOrganizationHandler(
		&organizationManagerStub{},
		&organizationMembershipManagerStub{},
		service.NewOrganizationFundingService(fundingRepo),
		nil,
	)
	router := gin.New()
	router.POST("/organizations/:id/credit", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Set(string(middleware.ContextKeyUserRole), service.RoleAdmin)
		c.Set("auth_method", "admin_api_key")
		c.Next()
	}, handler.Credit)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/organizations/7/credit", strings.NewReader(`{
		"amount":"1.00000000",
		"internal_reason":"must not run"
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "admin-key-credit")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Zero(t, fundingRepo.creditInput.OrganizationID)
	require.Contains(t, rec.Body.String(), "ORGANIZATION_FUNDING_FORBIDDEN")
}

func TestOrganizationHandlerRemoveMemberRequiresJWTAndReturnsSafeResult(t *testing.T) {
	membership := &organizationMembershipManagerStub{removeResult: &service.OrganizationMemberRemovalResult{
		OperationID: "operation-id", CorrelationID: "correlation-id", OrganizationID: 7, UserID: 11, AllocationID: 101,
		OperationType: domain.OrganizationFundingOperationMemberRemovalReclaim, Currency: domain.OrganizationCurrencyUSD,
		Outcome: service.OrganizationAllocationOutcomeCommitted, Reclaimed: "2.00000000", PoolBefore: "8.00000000", PoolAfter: "10.00000000",
		BalanceVersionBefore: 4, BalanceVersionAfter: 5, CanonicalPayloadHash: strings.Repeat("b", 64),
	}}
	handler := NewOrganizationHandler(&organizationManagerStub{}, membership, nil, nil)
	router := newOrganizationHandlerRouter(handler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/organizations/7/members/11", strings.NewReader(`{"public_reason":"offboarding"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "platform-remove-1")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(42), membership.removeActor.UserID)
	require.Equal(t, domain.OrganizationFundingAuthorizationPlatformAdminJWT, membership.removeActor.AuthorizationScope)
	require.Equal(t, int64(7), membership.removeOrgID)
	require.Equal(t, int64(11), membership.removeUserID)
	require.Equal(t, "platform-remove-1", membership.removeKey)
	require.Contains(t, rec.Body.String(), `"reclaimed":"2.00000000"`)
	require.NotContains(t, rec.Body.String(), "api_key_ids")

	adminKeyRouter := gin.New()
	adminKeyRouter.DELETE("/organizations/:id/members/:user_id", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Set(string(middleware.ContextKeyUserRole), service.RoleAdmin)
		c.Set("auth_method", "admin_api_key")
		c.Next()
	}, handler.RemoveMember)
	adminKeyRec := httptest.NewRecorder()
	adminKeyReq := httptest.NewRequest(http.MethodDelete, "/organizations/7/members/11", strings.NewReader(`{}`))
	adminKeyReq.Header.Set("Content-Type", "application/json")
	adminKeyReq.Header.Set("Idempotency-Key", "platform-remove-2")
	adminKeyRouter.ServeHTTP(adminKeyRec, adminKeyReq)

	require.Equal(t, http.StatusForbidden, adminKeyRec.Code)
	require.Contains(t, adminKeyRec.Body.String(), "ORGANIZATION_MEMBER_REMOVAL_FORBIDDEN")
}

func TestOrganizationHandlerBalanceAndLedgerAreOrganizationScoped(t *testing.T) {
	publicReason := "Quarterly allowance"
	fundingRepo := &organizationFundingRepositoryStub{
		summary: &service.OrganizationBalanceSummary{
			OrganizationID: 7,
			Status:         service.OrganizationStatusActive,
			Currency:       domain.OrganizationCurrencyUSD,
			Unallocated:    "8.00000000",
			Available:      "2.00000000",
			Reserved:       "0.50000000",
			Consumed:       "1.50000000",
			TotalRemaining: "10.50000000",
			BalanceVersion: 4,
		},
		ledger: []service.OrganizationLedgerEntry{{
			PostingID:      91,
			OperationID:    "op-1",
			OrganizationID: 7,
			Currency:       domain.OrganizationCurrencyUSD,
			PublicReason:   &publicReason,
			BeforeAmount:   "6.75000000",
			DeltaAmount:    "1.25000000",
			AfterAmount:    "8.00000000",
		}},
		ledgerPage: &pagination.PaginationResult{Total: 1, Page: 1, PageSize: 20, Pages: 1},
	}
	handler := NewOrganizationHandler(
		&organizationManagerStub{},
		&organizationMembershipManagerStub{},
		service.NewOrganizationFundingService(fundingRepo),
		nil,
	)
	router := newOrganizationHandlerRouter(handler)

	balanceRecorder := httptest.NewRecorder()
	router.ServeHTTP(balanceRecorder, httptest.NewRequest(http.MethodGet, "/organizations/7/balance", nil))
	require.Equal(t, http.StatusOK, balanceRecorder.Code)
	require.Equal(t, int64(7), fundingRepo.summaryOrgID)
	require.Contains(t, balanceRecorder.Body.String(), `"total_remaining":"10.50000000"`)

	ledgerRecorder := httptest.NewRecorder()
	router.ServeHTTP(ledgerRecorder, httptest.NewRequest(http.MethodGet, "/organizations/7/ledger?sort_order=asc", nil))
	require.Equal(t, http.StatusOK, ledgerRecorder.Code)
	require.Equal(t, int64(7), fundingRepo.ledgerOrgID)
	require.Equal(t, "asc", fundingRepo.ledgerParams.SortOrder)
	require.Contains(t, ledgerRecorder.Body.String(), `"public_reason":"Quarterly allowance"`)
}

func TestOrganizationHandlerPlatformAllocationRequiresJWTAndUsesTargetOrganization(t *testing.T) {
	allocation := &organizationAllocationManagerStub{
		preview: &service.OrganizationAllocationPreview{
			Confirmation: service.OrganizationAllocationConfirmation{
				OrganizationID: 7, AuthorizationScope: domain.OrganizationFundingAuthorizationPlatformAdminJWT,
				OperationType: domain.OrganizationFundingOperationCustomAllocation, Currency: domain.OrganizationCurrencyUSD,
				MoneyScale: 8, PoolBefore: "5.00000000", PoolAfter: "3.00000000", TotalDelta: "2.00000000",
				Targets: []service.OrganizationAllocationTargetPreview{{UserID: 11, AllocationID: 101, Before: "0.00000000", Delta: "2.00000000", After: "2.00000000"}},
			},
			PreviewToken: "platform-preview", CanonicalPayloadHash: strings.Repeat("a", 64),
		},
		result: &service.OrganizationAllocationResult{
			OperationID: "op-1", CorrelationID: "corr-1", OrganizationID: 7,
			OperationType: domain.OrganizationFundingOperationCustomAllocation, Currency: domain.OrganizationCurrencyUSD,
			Outcome: service.OrganizationAllocationOutcomeCommitted, PoolBefore: "5.00000000", PoolAfter: "3.00000000",
			TotalDelta: "2.00000000", BalanceVersionBefore: 1, BalanceVersionAfter: 2,
			Postings: []service.OrganizationLedgerEntry{{ActorIDSnapshot: 42, InternalReason: "hidden audit"}},
		},
	}
	handler := NewOrganizationHandler(&organizationManagerStub{}, &organizationMembershipManagerStub{}, nil, allocation)
	router := newOrganizationHandlerRouter(handler)

	previewRecorder := httptest.NewRecorder()
	previewRequest := httptest.NewRequest(http.MethodPost, "/organizations/7/allocations/custom/preview", strings.NewReader(`{"targets":[{"user_id":11,"amount":"2.00000000"}]}`))
	previewRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(previewRecorder, previewRequest)
	require.Equal(t, http.StatusOK, previewRecorder.Code)
	require.Equal(t, int64(7), allocation.previewOrgID)
	require.Equal(t, domain.OrganizationFundingOperationCustomAllocation, allocation.previewOp)
	require.Equal(t, domain.OrganizationFundingAuthorizationPlatformAdminJWT, allocation.previewActor.AuthorizationScope)

	executeRecorder := httptest.NewRecorder()
	executeRequest := httptest.NewRequest(http.MethodPost, "/organizations/7/allocations/custom", strings.NewReader(`{"preview_token":"platform-preview","confirmation":{"organization_id":7,"authorization_scope":"platform_admin_jwt","operation_type":"custom_allocation","currency":"USD","money_scale":8,"pool_before":"5.00000000","pool_after":"3.00000000","total_delta":"2.00000000","targets":[{"user_id":11,"allocation_id":101,"before":"0.00000000","delta":"2.00000000","after":"2.00000000"}]}}`))
	executeRequest.Header.Set("Content-Type", "application/json")
	executeRequest.Header.Set("Idempotency-Key", "platform-allocation-1")
	router.ServeHTTP(executeRecorder, executeRequest)
	require.Equal(t, http.StatusOK, executeRecorder.Code)
	require.Equal(t, "platform-allocation-1", allocation.executeKey)
	require.NotContains(t, executeRecorder.Body.String(), "hidden audit")
	require.NotContains(t, executeRecorder.Body.String(), "actor_id_snapshot")
}

func TestOrganizationHandlerPlatformAllocationRejectsAdminAPIKey(t *testing.T) {
	allocation := &organizationAllocationManagerStub{}
	handler := NewOrganizationHandler(&organizationManagerStub{}, &organizationMembershipManagerStub{}, nil, allocation)
	router := gin.New()
	router.POST("/organizations/:id/allocations/custom/preview", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Set(string(middleware.ContextKeyUserRole), service.RoleAdmin)
		c.Set("auth_method", "admin_api_key")
		c.Next()
	}, handler.PreviewCustomAllocation)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/organizations/7/allocations/custom/preview", strings.NewReader(`{"targets":[{"user_id":11,"amount":"2.00000000"}]}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Zero(t, allocation.previewOrgID)
	require.Contains(t, recorder.Body.String(), "ORGANIZATION_ALLOCATION_FORBIDDEN")
}
