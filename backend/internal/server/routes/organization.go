package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"

	"github.com/gin-gonic/gin"
)

// RegisterOrganizationRoutes registers the JWT-only current-organization
// boundary. The middleware derives the organization from current server state;
// no organization ID is accepted in the route or request body.
func RegisterOrganizationRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	organizationAdminAuth middleware.OrganizationAdminAuthMiddleware,
) {
	organization := v1.Group("/organization")
	organization.Use(gin.HandlerFunc(jwtAuth), organizationAdminAuth.Require)
	{
		organization.GET("", h.Organization.GetCurrent)
		organization.GET("/members", h.Organization.ListCurrentMembers)
		organization.GET("/report", h.Organization.GetCurrentReport)
		organization.GET("/report/members", h.Organization.ListCurrentReportMembers)
		organization.POST("/members", h.Organization.AddCurrentMember)
		organization.DELETE("/members/:user_id", h.Organization.RemoveCurrentMember)
	}
}
