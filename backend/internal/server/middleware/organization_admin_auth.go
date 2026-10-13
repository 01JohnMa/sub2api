package middleware

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// OrganizationScope is the server-derived current-organization identity.
// Handlers must not replace OrganizationID with a client-provided value.
type OrganizationScope struct {
	UserID           int64
	OrganizationID   int64
	OrganizationRole string
	Organization     service.Organization
}

type organizationAdminUserReader interface {
	GetByID(ctx context.Context, id int64) (*service.User, error)
}

// NewOrganizationAdminAuthMiddleware creates the JWT-only current-organization
// authorization layer. It must run after JWTAuthMiddleware.
func NewOrganizationAdminAuthMiddleware(
	userService *service.UserService,
	organizationService service.OrganizationReader,
) OrganizationAdminAuthMiddleware {
	return OrganizationAdminAuthMiddleware{
		Require:               organizationAdminAuth(userService, organizationService),
		PersonalAccountAccess: personalAccountAccess(userService, organizationService),
	}
}

func organizationAdminAuth(
	userReader organizationAdminUserReader,
	organizationReader service.OrganizationReader,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject, ok := GetAuthSubjectFromContext(c)
		if !ok || subject.UserID <= 0 {
			AbortWithError(c, 401, "UNAUTHORIZED", "User not authenticated")
			return
		}
		if userReader == nil || organizationReader == nil {
			AbortWithError(c, 500, "INTERNAL_ERROR", "Failed to authorize organization access")
			return
		}

		user, err := userReader.GetByID(c.Request.Context(), subject.UserID)
		if err != nil {
			if errors.Is(err, service.ErrUserNotFound) {
				AbortWithError(c, 401, "USER_NOT_FOUND", "User not found")
				return
			}
			AbortWithError(c, 500, "INTERNAL_ERROR", "Failed to authorize organization access")
			return
		}
		if user == nil || !user.IsActive() {
			AbortWithError(c, 401, "USER_INACTIVE", "User account is not active")
			return
		}
		if user.OrganizationID == nil || user.OrganizationRole == nil || *user.OrganizationRole != service.OrganizationRoleAdmin {
			AbortWithError(c, 403, "ORGANIZATION_ADMIN_REQUIRED", "Organization administrator access required")
			return
		}

		organization, err := organizationReader.Get(c.Request.Context(), *user.OrganizationID)
		if err != nil {
			if errors.Is(err, service.ErrOrganizationNotFound) {
				AbortWithError(c, 403, "ORGANIZATION_UNAVAILABLE", "Organization is unavailable")
				return
			}
			AbortWithError(c, 500, "INTERNAL_ERROR", "Failed to authorize organization access")
			return
		}
		if organization == nil || organization.ID != *user.OrganizationID || organization.Status != service.OrganizationStatusActive {
			AbortWithError(c, 403, "ORGANIZATION_UNAVAILABLE", "Organization is unavailable")
			return
		}

		c.Set(string(ContextKeyOrganizationScope), OrganizationScope{
			UserID:           user.ID,
			OrganizationID:   organization.ID,
			OrganizationRole: *user.OrganizationRole,
			Organization:     *organization,
		})
		c.Next()
	}
}

// personalAccountAccess preserves personal APIs for platform administrators,
// organization members, and unorganized users. Active organization
// administrators use the current-organization API instead.
func personalAccountAccess(
	userReader organizationAdminUserReader,
	organizationReader service.OrganizationReader,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject, ok := GetAuthSubjectFromContext(c)
		if !ok || subject.UserID <= 0 {
			AbortWithError(c, 401, "UNAUTHORIZED", "User not authenticated")
			return
		}
		if userReader == nil || organizationReader == nil {
			AbortWithError(c, 500, "INTERNAL_ERROR", "Failed to authorize personal account access")
			return
		}

		user, err := userReader.GetByID(c.Request.Context(), subject.UserID)
		if err != nil {
			if errors.Is(err, service.ErrUserNotFound) {
				AbortWithError(c, 401, "USER_NOT_FOUND", "User not found")
				return
			}
			AbortWithError(c, 500, "INTERNAL_ERROR", "Failed to authorize personal account access")
			return
		}
		if user == nil || !user.IsActive() {
			AbortWithError(c, 401, "USER_INACTIVE", "User account is not active")
			return
		}
		if user.IsAdmin() {
			c.Next()
			return
		}
		if user.OrganizationID == nil && user.OrganizationRole == nil {
			c.Next()
			return
		}
		if user.OrganizationID == nil || user.OrganizationRole == nil {
			AbortWithError(c, 403, "ORGANIZATION_MEMBERSHIP_INCONSISTENT", "Organization membership is inconsistent")
			return
		}
		switch *user.OrganizationRole {
		case service.OrganizationRoleMember:
			c.Next()
			return
		case service.OrganizationRoleAdmin:
			AbortWithError(c, 403, "ORGANIZATION_PERSONAL_ACCESS_FORBIDDEN", "Use the organization workspace for this operation")
			return
		default:
			AbortWithError(c, 403, "ORGANIZATION_MEMBERSHIP_INCONSISTENT", "Organization membership is inconsistent")
			return
		}
	}
}

// GetOrganizationScopeFromContext returns the server-derived current scope.
func GetOrganizationScopeFromContext(c *gin.Context) (OrganizationScope, bool) {
	value, exists := c.Get(string(ContextKeyOrganizationScope))
	if !exists {
		return OrganizationScope{}, false
	}
	scope, ok := value.(OrganizationScope)
	return scope, ok
}
