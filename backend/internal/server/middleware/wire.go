package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/wire"
)

// JWTAuthMiddleware JWT 认证中间件类型
type JWTAuthMiddleware gin.HandlerFunc

// AdminAuthMiddleware 管理员认证中间件类型
type AdminAuthMiddleware gin.HandlerFunc

// OrganizationAdminAuthMiddleware contains the two current-user boundary
// handlers derived from the same server-side User/Organization reader:
// Require authorizes organization APIs, while PersonalAccountAccess rejects
// personal-account APIs for active non-platform organization administrators.
type OrganizationAdminAuthMiddleware struct {
	Require               gin.HandlerFunc
	PersonalAccountAccess gin.HandlerFunc
}

// APIKeyAuthMiddleware API Key 认证中间件类型
type APIKeyAuthMiddleware gin.HandlerFunc

// ProviderSet 中间件层的依赖注入
var ProviderSet = wire.NewSet(
	NewJWTAuthMiddleware,
	NewAdminAuthMiddleware,
	NewOrganizationAdminAuthMiddleware,
	NewAPIKeyAuthMiddleware,
)
