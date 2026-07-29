//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type organizationMembershipManagerStub struct {
	member         *service.OrganizationMember
	err            error
	organizationID int64
	target         service.OrganizationMemberTarget
	removeActor    service.OrganizationMemberRemovalActor
	removeUserID   int64
	removeInput    service.OrganizationMemberRemovalInput
	removeKey      string
	removeResult   *service.OrganizationMemberRemovalResult
}

type organizationFundingReaderStub struct {
	summary      *service.OrganizationBalanceSummary
	summaryOrgID int64
	ledger       []service.OrganizationLedgerEntry
	ledgerOrgID  int64
	ledgerPage   *pagination.PaginationResult
	ledgerParams pagination.PaginationParams
}

func (s *organizationFundingReaderStub) GetSummary(_ context.Context, organizationID int64) (*service.OrganizationBalanceSummary, error) {
	s.summaryOrgID = organizationID
	return s.summary, nil
}

func (s *organizationFundingReaderStub) ListLedger(_ context.Context, organizationID int64, params pagination.PaginationParams) ([]service.OrganizationLedgerEntry, *pagination.PaginationResult, error) {
	s.ledgerOrgID = organizationID
	s.ledgerParams = params
	return s.ledger, s.ledgerPage, nil
}

type organizationAllocationManagerStub struct {
	previewInput  service.OrganizationAllocationPreviewInput
	previewActor  service.OrganizationAllocationActor
	previewOrgID  int64
	previewOp     string
	preview       *service.OrganizationAllocationPreview
	executeInput  service.OrganizationAllocationExecuteInput
	executeActor  service.OrganizationAllocationActor
	executeOrgID  int64
	executeOp     string
	executeKey    string
	executeResult *service.OrganizationAllocationResult
}

func (s *organizationAllocationManagerStub) Preview(_ context.Context, actor service.OrganizationAllocationActor, organizationID int64, operationType string, input service.OrganizationAllocationPreviewInput) (*service.OrganizationAllocationPreview, error) {
	s.previewActor, s.previewOrgID, s.previewOp, s.previewInput = actor, organizationID, operationType, input
	return s.preview, nil
}

func (s *organizationAllocationManagerStub) Execute(_ context.Context, actor service.OrganizationAllocationActor, organizationID int64, operationType string, input service.OrganizationAllocationExecuteInput, idempotencyKey string) (*service.OrganizationAllocationResult, error) {
	s.executeActor, s.executeOrgID, s.executeOp, s.executeInput, s.executeKey = actor, organizationID, operationType, input, idempotencyKey
	return s.executeResult, nil
}

func (s *organizationMembershipManagerStub) List(_ context.Context, organizationID int64, params pagination.PaginationParams) ([]service.OrganizationMember, *pagination.PaginationResult, error) {
	s.organizationID = organizationID
	if s.member == nil {
		return nil, nil, s.err
	}
	return []service.OrganizationMember{*s.member}, &pagination.PaginationResult{Total: 1, Page: params.Page, PageSize: params.PageSize, Pages: 1}, s.err
}

func (s *organizationMembershipManagerStub) Add(context.Context, int64, int64, string) (*service.OrganizationMember, error) {
	return nil, s.err
}

func (s *organizationMembershipManagerStub) AddCurrentMember(_ context.Context, organizationID int64, target service.OrganizationMemberTarget) (*service.OrganizationMember, error) {
	s.organizationID = organizationID
	s.target = target
	return s.member, s.err
}

func (s *organizationMembershipManagerStub) ChangeRole(context.Context, int64, int64, string) (*service.OrganizationMember, error) {
	return nil, s.err
}

func (s *organizationMembershipManagerStub) Remove(_ context.Context, actor service.OrganizationMemberRemovalActor, organizationID, targetUserID int64, input service.OrganizationMemberRemovalInput, idempotencyKey string) (*service.OrganizationMemberRemovalResult, error) {
	s.removeActor = actor
	s.organizationID = organizationID
	s.removeUserID = targetUserID
	s.removeInput = input
	s.removeKey = idempotencyKey
	return s.removeResult, s.err
}

func TestOrganizationHandlerGetCurrentReturnsSafeServerScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewOrganizationHandler(nil, nil, nil)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/organization?organization_id=99", nil)
	c.Set(string(middleware.ContextKeyOrganizationScope), middleware.OrganizationScope{
		UserID:           31,
		OrganizationID:   7,
		OrganizationRole: service.OrganizationRoleAdmin,
		Organization: service.Organization{
			ID:          7,
			Name:        "Acme",
			Code:        "acme",
			Description: "Platform team",
			Status:      service.OrganizationStatusActive,
			CreatedBy:   99,
		},
	})

	handler.GetCurrent(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var responseBody struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &responseBody))
	require.Equal(t, float64(7), responseBody.Data["id"])
	require.Equal(t, "Acme", responseBody.Data["name"])
	require.NotContains(t, responseBody.Data, "created_by")
}

func TestOrganizationHandlerGetCurrentRequiresScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewOrganizationHandler(nil, nil, nil)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/organization", nil)

	handler.GetCurrent(c)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestOrganizationHandlerAddCurrentMemberUsesServerScopeAndSafeMemberDTO(t *testing.T) {
	gin.SetMode(gin.TestMode)
	membership := &organizationMembershipManagerStub{member: &service.OrganizationMember{
		UserID: 7, Email: "member@example.com", OrganizationRole: service.OrganizationRoleMember,
		Allocation: service.OrganizationMemberAllocation{ID: 11, Status: "active", Available: "0.00000000", Reserved: "0.00000000"},
	}}
	handler := NewOrganizationHandler(membership, nil, nil)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/organization/members", strings.NewReader(`{"email":" Member@Example.COM ","organization_id":99,"organization_role":"organization_admin"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyOrganizationScope), middleware.OrganizationScope{OrganizationID: 3, OrganizationRole: service.OrganizationRoleAdmin})

	handler.AddCurrentMember(c)

	require.Equal(t, http.StatusCreated, recorder.Code)
	require.Equal(t, int64(3), membership.organizationID)
	require.Nil(t, membership.target.UserID)
	require.Equal(t, "Member@Example.COM", membership.target.Email)
	require.Contains(t, recorder.Body.String(), `"organization_role":"organization_member"`)
	require.NotContains(t, recorder.Body.String(), "organization_id")
}

func TestOrganizationHandlerAddCurrentMemberUsesSameIneligibleEnvelopeForTargets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	bodyByTarget := make(map[string]string)
	for name, body := range map[string]string{
		"nonexistent": `{"email":"missing@example.com"}`,
		"organized":   `{"email":"organized@example.com"}`,
		"deleted":     `{"email":"deleted@example.com"}`,
		"allocation":  `{"email":"allocation@example.com"}`,
	} {
		t.Run(name, func(t *testing.T) {
			membership := &organizationMembershipManagerStub{err: service.ErrOrganizationMemberIneligible}
			handler := NewOrganizationHandler(membership, nil, nil)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/organization/members", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware.ContextKeyOrganizationScope), middleware.OrganizationScope{OrganizationID: 3, OrganizationRole: service.OrganizationRoleAdmin})

			handler.AddCurrentMember(c)

			require.Equal(t, http.StatusConflict, recorder.Code)
			bodyByTarget[name] = recorder.Body.String()
		})
	}
	require.Equal(t, bodyByTarget["nonexistent"], bodyByTarget["organized"])
	require.Equal(t, bodyByTarget["nonexistent"], bodyByTarget["deleted"])
	require.Equal(t, bodyByTarget["nonexistent"], bodyByTarget["allocation"])
}

func TestOrganizationHandlerAddCurrentMemberRejectsAmbiguousTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	membership := &organizationMembershipManagerStub{}
	handler := NewOrganizationHandler(membership, nil, nil)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/organization/members", strings.NewReader(`{"user_id":7,"email":"member@example.com"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyOrganizationScope), middleware.OrganizationScope{OrganizationID: 3, OrganizationRole: service.OrganizationRoleAdmin})

	handler.AddCurrentMember(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, membership.organizationID)
}

func organizationAdminScopeContext(c *gin.Context) {
	c.Set("auth_method", "jwt")
	c.Set(string(middleware.ContextKeyOrganizationScope), middleware.OrganizationScope{
		UserID: 31, OrganizationID: 7, OrganizationRole: service.OrganizationRoleAdmin,
	})
}

func TestOrganizationHandlerCurrentMembersAndBalanceUseServerScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	membership := &organizationMembershipManagerStub{member: &service.OrganizationMember{
		UserID: 11, Email: "member@example.com", OrganizationRole: service.OrganizationRoleMember,
		Allocation: service.OrganizationMemberAllocation{ID: 101, Status: "active", Available: "2.00000000", Reserved: "0.00000000"},
	}}
	funding := &organizationFundingReaderStub{summary: &service.OrganizationBalanceSummary{
		OrganizationID: 7, Status: domain.OrganizationStatusActive, Currency: domain.OrganizationCurrencyUSD,
		Unallocated: "8.00000000", Available: "2.00000000", Reserved: "0.00000000", Consumed: "0.00000000",
		TotalRemaining: "10.00000000", BalanceVersion: 4,
	}, ledgerPage: &pagination.PaginationResult{Page: 1, PageSize: 20}}
	handler := NewOrganizationHandler(membership, funding, nil)

	for _, request := range []*http.Request{
		httpRequestWithContext(http.MethodGet, "/organization/members?page=2", ""),
		httpRequestWithContext(http.MethodGet, "/organization/balance?organization_id=99", ""),
	} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = request
		organizationAdminScopeContext(c)
		if strings.Contains(request.URL.Path, "/members") {
			handler.ListCurrentMembers(c)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, int64(7), membership.organizationID)
		} else {
			handler.GetCurrentBalance(c)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, int64(7), funding.summaryOrgID)
			require.Contains(t, recorder.Body.String(), `"unallocated":"8.00000000"`)
		}
	}
}

func TestOrganizationHandlerRemoveCurrentMemberUsesServerScopeAndIdempotency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	membership := &organizationMembershipManagerStub{removeResult: &service.OrganizationMemberRemovalResult{
		OperationID: "operation-id", CorrelationID: "correlation-id", OrganizationID: 7, UserID: 11, AllocationID: 101,
		OperationType: domain.OrganizationFundingOperationMemberRemovalReclaim, Currency: domain.OrganizationCurrencyUSD,
		Outcome: service.OrganizationAllocationOutcomeCommitted, Reclaimed: "3.00000000", PoolBefore: "10.00000000", PoolAfter: "13.00000000",
		BalanceVersionBefore: 3, BalanceVersionAfter: 4, CanonicalPayloadHash: strings.Repeat("a", 64), Replayed: true,
	}}
	handler := NewOrganizationHandler(membership, nil, nil)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/organization/members/11", strings.NewReader(`{"public_reason":" offboarding "}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Idempotency-Key", "member-remove-1")
	c.Params = gin.Params{{Key: "user_id", Value: "11"}}
	organizationAdminScopeContext(c)

	handler.RemoveCurrentMember(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(7), membership.organizationID)
	require.Equal(t, int64(11), membership.removeUserID)
	require.Equal(t, int64(31), membership.removeActor.UserID)
	require.Equal(t, domain.OrganizationFundingAuthorizationOrganizationJWT, membership.removeActor.AuthorizationScope)
	require.Equal(t, "offboarding", membership.removeInput.PublicReason)
	require.Equal(t, "member-remove-1", membership.removeKey)
	require.Equal(t, "true", recorder.Header().Get("X-Idempotency-Replayed"))
	require.Contains(t, recorder.Body.String(), `"reclaimed":"3.00000000"`)
	require.NotContains(t, recorder.Body.String(), "api_key_ids")
}

func httpRequestWithContext(method, path, body string) *http.Request {
	return httptest.NewRequest(method, path, strings.NewReader(body))
}

func TestOrganizationHandlerAllocationPreviewAndExecuteUseFixedOperationAndSafeResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := &organizationAllocationManagerStub{
		preview: &service.OrganizationAllocationPreview{
			Confirmation: service.OrganizationAllocationConfirmation{
				OrganizationID: 7, AuthorizationScope: domain.OrganizationFundingAuthorizationOrganizationJWT,
				OperationType: domain.OrganizationFundingOperationCustomAllocation, Currency: domain.OrganizationCurrencyUSD,
				MoneyScale: 8, PoolBefore: "10.00000000", PoolAfter: "8.00000000", TotalDelta: "2.00000000",
				Targets: []service.OrganizationAllocationTargetPreview{{UserID: 11, AllocationID: 101, Before: "0.00000000", Delta: "2.00000000", After: "2.00000000"}},
			},
			PreviewToken: "preview-token", CanonicalPayloadHash: strings.Repeat("a", 64),
		},
		executeResult: &service.OrganizationAllocationResult{
			OperationID: "op-1", CorrelationID: "corr-1", OrganizationID: 7,
			OperationType: domain.OrganizationFundingOperationCustomAllocation, Currency: domain.OrganizationCurrencyUSD,
			Outcome: service.OrganizationAllocationOutcomeCommitted, PoolBefore: "10.00000000", PoolAfter: "8.00000000",
			TotalDelta: "2.00000000", BalanceVersionBefore: 4, BalanceVersionAfter: 5, Replayed: true,
			Postings: []service.OrganizationLedgerEntry{{ActorIDSnapshot: 31, InternalReason: "must not serialize"}},
		},
	}
	handler := NewOrganizationHandler(nil, nil, manager)
	previewRecorder := httptest.NewRecorder()
	previewCtx, _ := gin.CreateTestContext(previewRecorder)
	previewCtx.Request = httptest.NewRequest(http.MethodPost, "/organization/allocations/custom/preview", strings.NewReader(`{"targets":[{"user_id":11,"amount":"2.00000000"}]}`))
	previewCtx.Request.Header.Set("Content-Type", "application/json")
	organizationAdminScopeContext(previewCtx)
	handler.PreviewCustomAllocation(previewCtx)
	require.Equal(t, http.StatusOK, previewRecorder.Code)
	require.Equal(t, int64(7), manager.previewOrgID)
	require.Equal(t, domain.OrganizationFundingOperationCustomAllocation, manager.previewOp)
	require.Equal(t, "2.00000000", manager.previewInput.Targets[0].Amount)

	executeRecorder := httptest.NewRecorder()
	executeCtx, _ := gin.CreateTestContext(executeRecorder)
	executeCtx.Request = httptest.NewRequest(http.MethodPost, "/organization/allocations/custom", strings.NewReader(`{"preview_token":"preview-token","confirmation":{"organization_id":7,"authorization_scope":"organization_admin_jwt","operation_type":"custom_allocation","currency":"USD","money_scale":8,"pool_before":"10.00000000","pool_after":"8.00000000","total_delta":"2.00000000","targets":[{"user_id":11,"allocation_id":101,"before":"0.00000000","delta":"2.00000000","after":"2.00000000"}]}}`))
	executeCtx.Request.Header.Set("Content-Type", "application/json")
	executeCtx.Request.Header.Set("Idempotency-Key", "allocation-retry-1")
	organizationAdminScopeContext(executeCtx)
	handler.ExecuteCustomAllocation(executeCtx)
	require.Equal(t, http.StatusOK, executeRecorder.Code)
	require.Equal(t, "allocation-retry-1", manager.executeKey)
	require.Equal(t, domain.OrganizationFundingOperationCustomAllocation, manager.executeOp)
	require.Contains(t, executeRecorder.Header().Get("X-Idempotency-Replayed"), "true")
	require.NotContains(t, executeRecorder.Body.String(), "must not serialize")
	require.NotContains(t, executeRecorder.Body.String(), "actor_id_snapshot")
}
