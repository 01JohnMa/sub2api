package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

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
	require.True(t, registered["GET /api/v1/admin/organizations/:id/members"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/members"])
	require.True(t, registered["PUT /api/v1/admin/organizations/:id/members/:user_id"])
	require.True(t, registered["DELETE /api/v1/admin/organizations/:id/members/:user_id"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/credit"])
	require.True(t, registered["GET /api/v1/admin/organizations/:id/balance"])
	require.True(t, registered["GET /api/v1/admin/organizations/:id/ledger"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/allocations/custom/preview"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/allocations/custom"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/allocations/reclaim/preview"])
	require.True(t, registered["POST /api/v1/admin/organizations/:id/allocations/reclaim"])
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
	require.True(t, registered["GET /api/v1/organization/balance"])
	require.True(t, registered["POST /api/v1/organization/members"])
	require.True(t, registered["DELETE /api/v1/organization/members/:user_id"])
	require.True(t, registered["POST /api/v1/organization/allocations/equal/preview"])
	require.True(t, registered["POST /api/v1/organization/allocations/equal"])
	require.True(t, registered["POST /api/v1/organization/allocations/custom/preview"])
	require.True(t, registered["POST /api/v1/organization/allocations/custom"])
	require.True(t, registered["POST /api/v1/organization/allocations/reclaim/preview"])
	require.True(t, registered["POST /api/v1/organization/allocations/reclaim"])
	require.False(t, registered["POST /api/v1/organization"])
	require.False(t, registered["PUT /api/v1/organization"])
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
