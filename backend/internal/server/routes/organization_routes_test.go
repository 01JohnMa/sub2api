package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type currentOrganizationRoutesMembershipStub struct {
	organizationID int64
}

func (s *currentOrganizationRoutesMembershipStub) List(_ context.Context, organizationID int64, _ pagination.PaginationParams) ([]service.OrganizationMember, *pagination.PaginationResult, error) {
	s.organizationID = organizationID
	return []service.OrganizationMember{{
		UserID: 11, Email: "member@example.com", OrganizationRole: service.OrganizationRoleMember,
		Allocation: service.OrganizationMemberAllocation{ID: 101, Status: "active", Available: "1.00000000", Reserved: "0.00000000"},
	}}, &pagination.PaginationResult{Total: 2, Page: 2, PageSize: 1, Pages: 2}, nil
}

func (*currentOrganizationRoutesMembershipStub) AddCurrentMember(context.Context, int64, service.OrganizationMemberTarget) (*service.OrganizationMember, error) {
	return nil, service.ErrOrganizationMemberIneligible
}

type currentOrganizationRoutesFundingStub struct {
	organizationID int64
	reportOrgID    int64
	report         *service.OrganizationReport
}

type currentOrganizationRoutesRecoveryStub struct {
	organizationID int64
}

func (*currentOrganizationRoutesRecoveryStub) RecordPendingAttempt(context.Context, service.OrganizationPendingAttemptCommand) error {
	return nil
}
func (*currentOrganizationRoutesRecoveryStub) ReconcilePendingExecution(context.Context, service.OrganizationPendingReconciliationCommand) (*service.OrganizationFinalExecutionEvidenceResult, error) {
	return nil, service.ErrOrganizationExecutionPendingReconciliation
}
func (s *currentOrganizationRoutesRecoveryStub) ListPendingReconciliations(_ context.Context, organizationID int64, _ service.OrganizationPendingReconciliationListParams) ([]service.OrganizationPendingReconciliation, *service.OrganizationPendingReconciliationPage, error) {
	s.organizationID = organizationID
	return nil, &service.OrganizationPendingReconciliationPage{Total: 2, Page: 1, PageSize: 1, Pages: 2}, nil
}
func (*currentOrganizationRoutesRecoveryStub) GetPendingReconciliation(context.Context, int64, string) (*service.OrganizationPendingReconciliation, error) {
	return nil, service.ErrOrganizationExecutionConflict
}
func (*currentOrganizationRoutesRecoveryStub) RedriveAttemptAccountEffect(context.Context, string) (*service.OrganizationAttemptAccountEffectResult, error) {
	return nil, nil
}
func (*currentOrganizationRoutesRecoveryStub) RedrivePendingAttemptAccountEffects(context.Context, int) ([]service.OrganizationAttemptAccountEffectResult, error) {
	return nil, nil
}
func (*currentOrganizationRoutesRecoveryStub) RedriveExecutionOutbox(context.Context, int64) (*service.OrganizationExecutionOutboxApplyResult, error) {
	return nil, nil
}

func (s *currentOrganizationRoutesFundingStub) GetSummary(_ context.Context, organizationID int64) (*service.OrganizationBalanceSummary, error) {
	s.organizationID = organizationID
	return &service.OrganizationBalanceSummary{OrganizationID: organizationID, Status: service.OrganizationStatusActive, Currency: "USD", Unallocated: "3.00000000", Available: "1.00000000", Reserved: "0.00000000", Consumed: "0.00000000", TotalRemaining: "4.00000000"}, nil
}

func (*currentOrganizationRoutesFundingStub) ListLedger(context.Context, int64, pagination.PaginationParams) ([]service.OrganizationLedgerEntry, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (s *currentOrganizationRoutesFundingStub) GetReport(_ context.Context, organizationID int64, _ service.OrganizationReportRange) (*service.OrganizationReport, error) {
	s.reportOrgID = organizationID
	if s.report != nil {
		return s.report, nil
	}
	return &service.OrganizationReport{OrganizationID: organizationID, Cost: "0.00000000"}, nil
}

func (*currentOrganizationRoutesFundingStub) ListReportMembers(context.Context, int64, service.OrganizationReportRange, pagination.PaginationParams) ([]service.OrganizationReportMemberUsage, *service.OrganizationReportMemberUsage, *pagination.PaginationResult, error) {
	return nil, nil, nil, nil
}

func TestOrganizationRoutesExposeMembershipRemovalWithoutOrganizationDelete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{Organization: admin.NewOrganizationHandler(nil, nil, nil, nil)}}
	registerOrganizationRoutes(router.Group("/api/v1/admin"), handlers)

	routes := router.Routes()
	registered := make(map[string]bool, len(routes))
	for _, route := range routes {
		registered[route.Method+" "+route.Path] = true
	}

	require.True(t, registered["GET /api/v1/admin/organizations"])
	require.True(t, registered["POST /api/v1/admin/organizations"])
	require.True(t, registered["GET /api/v1/admin/organizations/:id"])
	require.True(t, registered["PUT /api/v1/admin/organizations/:id"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/disable"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/reactivate"])
	require.True(t, registered["GET /api/v1/admin/organizations/:id/archive-preview"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/archive"])
	require.True(t, registered["GET /api/v1/admin/organizations/:id/members"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/members"])
	require.True(t, registered["PUT /api/v1/admin/organizations/:id/members/:user_id"])
	require.True(t, registered["DELETE /api/v1/admin/organizations/:id/members/:user_id"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/credit"])
	require.True(t, registered["GET /api/v1/admin/organizations/:id/balance"])
	require.True(t, registered["GET /api/v1/admin/organizations/:id/report"])
	require.True(t, registered["GET /api/v1/admin/organizations/:id/report/members"])
	require.True(t, registered["GET /api/v1/admin/organizations/:id/ledger"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/allocations/custom/preview"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/allocations/custom"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/allocations/reclaim/preview"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/allocations/reclaim"])
	require.True(t, registered["GET /api/v1/admin/organizations/:id/pending-reconciliations"])
	require.True(t, registered["GET /api/v1/admin/organizations/:id/pending-reconciliations/:execution_id"])
	require.False(t, registered["POST /api/v1/admin/organizations/:id/pending-reconciliations/:execution_id/evidence"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/pending-reconciliations/:execution_id/resolve"])
	require.False(t, registered["DELETE /api/v1/admin/organizations/:id"])
}

func TestCurrentOrganizationRoutesAreJWTOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Organization: handler.NewOrganizationHandler(nil, nil, nil)}
	jwtAuth := middleware.JWTAuthMiddleware(func(c *gin.Context) { c.Next() })
	organizationAuth := middleware.OrganizationAdminAuthMiddleware{
		Require:               func(c *gin.Context) { c.Next() },
		PersonalAccountAccess: func(c *gin.Context) { c.Next() },
	}
	RegisterOrganizationRoutes(router.Group("/api/v1"), handlers, jwtAuth, organizationAuth)

	routes := router.Routes()
	registered := make(map[string]bool, len(routes))
	for _, route := range routes {
		registered[route.Method+" "+route.Path] = true
	}

	require.True(t, registered["GET /api/v1/organization"])
	require.True(t, registered["GET /api/v1/organization/members"])
	require.False(t, registered["GET /api/v1/organization/balance"])
	require.True(t, registered["GET /api/v1/organization/report"])
	require.True(t, registered["GET /api/v1/organization/report/members"])
	require.False(t, registered["GET /api/v1/organization/api-keys"])
	require.False(t, registered["GET /api/v1/organization/ledger"])
	require.False(t, registered["GET /api/v1/organization/pending-reconciliations/summary"])
	require.True(t, registered["POST /api/v1/organization/members"])
	require.True(t, registered["DELETE /api/v1/organization/members/:user_id"])
	require.False(t, registered["POST /api/v1/organization/allocations/equal/preview"])
	require.False(t, registered["POST /api/v1/organization/allocations/equal"])
	require.False(t, registered["POST /api/v1/organization/allocations/custom/preview"])
	require.False(t, registered["POST /api/v1/organization/allocations/custom"])
	require.False(t, registered["POST /api/v1/organization/allocations/reclaim/preview"])
	require.False(t, registered["POST /api/v1/organization/allocations/reclaim"])
	require.False(t, registered["POST /api/v1/organization"])
	require.False(t, registered["PUT /api/v1/organization"])
}

func TestCurrentOrganizationPendingSummaryUsesOnlyServerDerivedScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Organization: handler.NewOrganizationHandler(nil, nil, nil)}
	jwtAuth := middleware.JWTAuthMiddleware(func(c *gin.Context) { c.Set("auth_method", "jwt"); c.Next() })
	organizationAuth := middleware.OrganizationAdminAuthMiddleware{Require: func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyOrganizationScope), middleware.OrganizationScope{UserID: 31, OrganizationID: 7, OrganizationRole: service.OrganizationRoleAdmin})
		c.Next()
	}}
	RegisterOrganizationRoutes(router.Group("/api/v1"), handlers, jwtAuth, organizationAuth)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/organization/pending-reconciliations/summary?organization_id=99", nil))
	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestCurrentOrganizationReportUsesServerScopeAndExplicitHalfOpenRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	funding := &currentOrganizationRoutesFundingStub{report: &service.OrganizationReport{OrganizationID: 7, RequestCountTotal: 3, Cost: "1.00000000"}}
	handlers := &handler.Handlers{Organization: handler.NewOrganizationHandler(nil, funding, nil)}
	router := gin.New()
	jwtAuth := middleware.JWTAuthMiddleware(func(c *gin.Context) { c.Set("auth_method", "jwt"); c.Next() })
	organizationAuth := middleware.OrganizationAdminAuthMiddleware{Require: func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyOrganizationScope), middleware.OrganizationScope{UserID: 31, OrganizationID: 7, OrganizationRole: service.OrganizationRoleAdmin})
		c.Next()
	}}
	RegisterOrganizationRoutes(router.Group("/api/v1"), handlers, jwtAuth, organizationAuth)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/organization/report?organization_id=99&start=2026-07-01T00:00:00Z&end=2026-08-01T00:00:00Z", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(7), funding.reportOrgID)
	require.Contains(t, recorder.Body.String(), `"request_count_total":3`)
}

func TestCurrentOrganizationRoutesUseServerScopeAndRejectAbsentMutations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	members := &currentOrganizationRoutesMembershipStub{}
	funding := &currentOrganizationRoutesFundingStub{}
	handlers := &handler.Handlers{Organization: handler.NewOrganizationHandler(members, funding, nil)}
	jwtAuth := middleware.JWTAuthMiddleware(func(c *gin.Context) {
		c.Set("auth_method", "jwt")
		c.Next()
	})
	organizationAuth := middleware.OrganizationAdminAuthMiddleware{Require: func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyOrganizationScope), middleware.OrganizationScope{
			UserID: 31, OrganizationID: 7, OrganizationRole: service.OrganizationRoleAdmin,
			Organization: service.Organization{ID: 7, Status: service.OrganizationStatusActive},
		})
		c.Next()
	}}
	RegisterOrganizationRoutes(router.Group("/api/v1"), handlers, jwtAuth, organizationAuth)

	membersRecorder := httptest.NewRecorder()
	router.ServeHTTP(membersRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/organization/members?organization_id=99&page=2&page_size=1", nil))
	require.Equal(t, http.StatusOK, membersRecorder.Code)
	require.Equal(t, int64(7), members.organizationID)
	require.Contains(t, membersRecorder.Body.String(), `"total":2`)

	removedRouteRecorder := httptest.NewRecorder()
	router.ServeHTTP(removedRouteRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/organization/balance?organization_id=99", nil))
	require.Equal(t, http.StatusNotFound, removedRouteRecorder.Code)

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/organization/api-keys", nil),
		httptest.NewRequest(http.MethodPut, "/api/v1/organization", nil),
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusNotFound, recorder.Code)
	}
}

func TestPersonalAccountRoutesApplyOrganizationAdminBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")

	jwtAuth := middleware.JWTAuthMiddleware(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 31})
		c.Next()
	})
	adminAuth := middleware.AdminAuthMiddleware(func(c *gin.Context) { c.Next() })
	organizationAuth := middleware.OrganizationAdminAuthMiddleware{
		Require: func(c *gin.Context) { c.Next() },
		PersonalAccountAccess: func(c *gin.Context) {
			switch c.GetHeader("X-Test-Actor") {
			case "active_organization_admin":
				middleware.AbortWithError(
					c,
					http.StatusForbidden,
					"ORGANIZATION_PERSONAL_ACCESS_FORBIDDEN",
					"Use the organization workspace for this operation",
				)
			case "platform_admin", "organization_member", "unorganized_user":
				c.AbortWithStatus(http.StatusNoContent)
			default:
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		},
	}

	handlers := &handler.Handlers{}
	RegisterUserRoutes(v1, handlers, jwtAuth, organizationAuth, nil)
	RegisterPaymentRoutes(v1, nil, nil, nil, jwtAuth, adminAuth, organizationAuth, nil)

	personalRoutes := []string{
		"/api/v1/keys",
		"/api/v1/user/profile",
		"/api/v1/subscriptions",
		"/api/v1/payment/config",
	}
	actors := []struct {
		name       string
		header     string
		wantStatus int
		wantCode   string
	}{
		{name: "active organization admin", header: "active_organization_admin", wantStatus: http.StatusForbidden, wantCode: "ORGANIZATION_PERSONAL_ACCESS_FORBIDDEN"},
		{name: "platform admin", header: "platform_admin", wantStatus: http.StatusNoContent},
		{name: "organization member", header: "organization_member", wantStatus: http.StatusNoContent},
		{name: "unorganized user", header: "unorganized_user", wantStatus: http.StatusNoContent},
	}

	for _, path := range personalRoutes {
		for _, actor := range actors {
			t.Run(actor.name+" "+path, func(t *testing.T) {
				request := httptest.NewRequest(http.MethodGet, path, nil)
				request.Header.Set("X-Test-Actor", actor.header)
				recorder := httptest.NewRecorder()

				router.ServeHTTP(recorder, request)

				require.Equal(t, actor.wantStatus, recorder.Code)
				if actor.wantCode == "" {
					return
				}
				var body middleware.ErrorResponse
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
				require.Equal(t, actor.wantCode, body.Code)
			})
		}
	}
}
