package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// NewAPIKeyAuthMiddleware 创建 API Key 认证中间件
func NewAPIKeyAuthMiddleware(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) APIKeyAuthMiddleware {
	return APIKeyAuthMiddleware(apiKeyAuthWithSubscription(
		apiKeyService,
		subscriptionService,
		cfg,
		service.NewOrganizationSpendingCapabilityRegistry(cfg),
	))
}

// apiKeyAuthWithSubscription API Key认证中间件（支持订阅验证）
//
// 中间件职责分为两层：
//   - 鉴权（Authentication）：验证 Key 有效性、用户状态、IP 限制 —— 始终执行
//   - 计费执行（Billing Enforcement）：过期/配额/订阅/余额检查 —— skipBilling 时整块跳过
//
// /v1/usage 端点只需鉴权，不需要计费执行（允许过期/配额耗尽的 Key 查询自身用量）。
func apiKeyAuthWithSubscription(
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	cfg *config.Config,
	organizationSpending *service.OrganizationSpendingCapabilityRegistry,
) gin.HandlerFunc {
	_ = organizationSpending
	return func(c *gin.Context) {
		// ── 1. 提取 API Key ──────────────────────────────────────────

		queryKey := strings.TrimSpace(c.Query("key"))
		queryApiKey := strings.TrimSpace(c.Query("api_key"))
		if queryKey != "" || queryApiKey != "" {
			AbortWithError(c, 400, "api_key_in_query_deprecated", "API key in query parameter is deprecated. Please use Authorization header instead.")
			return
		}

		// 尝试从Authorization header中提取API key (Bearer scheme)
		authHeader := c.GetHeader("Authorization")
		var apiKeyString string

		if authHeader != "" {
			// 验证Bearer scheme
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				apiKeyString = strings.TrimSpace(parts[1])
			}
		}

		// 如果Authorization header中没有，尝试从x-api-key header中提取
		if apiKeyString == "" {
			apiKeyString = c.GetHeader("x-api-key")
		}

		// 如果x-api-key header中没有，尝试从x-goog-api-key header中提取（Gemini CLI兼容）
		if apiKeyString == "" {
			apiKeyString = c.GetHeader("x-goog-api-key")
		}

		// 如果所有header都没有API key
		if apiKeyString == "" {
			AbortWithError(c, 401, "API_KEY_REQUIRED", "API key is required in Authorization header (Bearer scheme), x-api-key header, or x-goog-api-key header")
			return
		}

		// ── 2. 验证 Key 存在 ─────────────────────────────────────────

		apiKey, err := apiKeyService.GetByKeyAuthoritative(c.Request.Context(), apiKeyString)
		if err != nil {
			abortAPIKeyResolutionError(c, err, false)
			return
		}

		// apiKey 已加载（含 User/Group）。即便后续因分组停用/Key 停用/用户停用/
		// IP 限制等早退中断，也让 Ops 错误日志能回退取到 user/group/platform。
		SetOpsFallbackAPIKey(c, apiKey)

		// ── 3. 基础鉴权（始终执行） ─────────────────────────────────

		// disabled / 未知状态 → 无条件拦截（expired 和 quota_exhausted 留给计费阶段）
		if !apiKey.IsActive() &&
			apiKey.Status != service.StatusAPIKeyExpired &&
			apiKey.Status != service.StatusAPIKeyQuotaExhausted {
			AbortWithError(c, 401, "API_KEY_DISABLED", "API key is disabled")
			return
		}

		// 检查 IP 限制（白名单/黑名单）
		// 注意：错误信息故意模糊，避免暴露具体的 IP 限制机制
		if len(apiKey.IPWhitelist) > 0 || len(apiKey.IPBlacklist) > 0 {
			clientIP := ip.GetTrustedClientIP(c)
			if cfg.TrustForwardedIPForAPIKeyACL() {
				clientIP = ip.GetClientIP(c)
			}
			allowed, _ := ip.CheckIPRestrictionWithCompiledRules(clientIP, apiKey.CompiledIPWhitelist, apiKey.CompiledIPBlacklist)
			if !allowed {
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonIPRestriction)
				AbortWithError(c, 403, "ACCESS_DENIED", "Access denied")
				return
			}
		}

		// 检查关联的用户
		if apiKey.User == nil {
			AbortWithError(c, 401, "USER_NOT_FOUND", "User associated with API key not found")
			return
		}

		// 检查用户状态
		if !apiKey.User.IsActive() {
			AbortWithError(c, 401, "USER_INACTIVE", "User account is not active")
			return
		}
		setOrganizationReportingContext(c, apiKey)
		if abortIfAPIKeyGroupUnavailable(c, apiKey) {
			return
		}
		if abortIfAPIKeyGroupNotAllowed(c, apiKey) {
			return
		}

		// ── 4. SimpleMode → early return ─────────────────────────────

		if cfg.RunMode == config.RunModeSimple {
			c.Set(string(ContextKeyAPIKey), apiKey)
			c.Set(string(ContextKeyUser), AuthSubject{
				UserID:      apiKey.User.ID,
				Concurrency: apiKey.User.Concurrency,
			})
			c.Set(string(ContextKeyUserRole), apiKey.User.Role)
			setGroupContext(c, apiKey.Group)
			_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
			c.Next()
			return
		}

		// ── 5. 加载订阅（订阅模式时始终加载） ───────────────────────

		// skipBilling: /v1/usage 只需鉴权，跳过所有计费执行
		skipBilling := c.Request.URL.Path == "/v1/usage"

		var subscription *service.UserSubscription
		isSubscriptionType := apiKey.Group != nil && apiKey.Group.IsSubscriptionType()

		if isSubscriptionType && subscriptionService != nil {
			sub, subErr := subscriptionService.GetActiveSubscription(
				c.Request.Context(),
				apiKey.User.ID,
				apiKey.Group.ID,
			)
			if subErr != nil {
				if !skipBilling {
					AbortWithError(c, 403, "SUBSCRIPTION_NOT_FOUND", "No active subscription found for this group")
					return
				}
				// skipBilling: 订阅不存在也放行，handler 会返回可用的数据
			} else {
				subscription = sub
			}
		}

		// ── 6. 计费执行（skipBilling 时整块跳过） ────────────────────

		if !skipBilling {
			if abortIfAPIKeyBillingStateInvalid(c, apiKey, false) {
				return
			}

			// 订阅模式：验证订阅限额
			if subscription != nil {
				needsMaintenance, validateErr := subscriptionService.ValidateAndCheckLimits(subscription, apiKey.Group)
				if validateErr != nil {
					code := "SUBSCRIPTION_INVALID"
					status := 403
					if errors.Is(validateErr, service.ErrDailyLimitExceeded) ||
						errors.Is(validateErr, service.ErrWeeklyLimitExceeded) ||
						errors.Is(validateErr, service.ErrMonthlyLimitExceeded) {
						code = "USAGE_LIMIT_EXCEEDED"
						status = 429
					}
					AbortWithError(c, status, code, validateErr.Error())
					return
				}

				// 窗口维护异步化（不阻塞请求）
				if needsMaintenance {
					maintenanceCopy := *subscription
					subscriptionService.DoWindowMaintenance(&maintenanceCopy)
				}
			} else {
				// 非订阅模式 或 订阅模式但 subscriptionService 未注入：回退到余额检查
				if apiKey.User.Balance <= 0 {
					AbortWithError(c, 403, "INSUFFICIENT_BALANCE", "Insufficient account balance")
					return
				}
			}
		}

		// ── 7. 设置上下文 → Next ─────────────────────────────────────

		if subscription != nil {
			c.Set(string(ContextKeySubscription), subscription)
		}
		c.Set(string(ContextKeyAPIKey), apiKey)
		c.Set(string(ContextKeyUser), AuthSubject{
			UserID:      apiKey.User.ID,
			Concurrency: apiKey.User.Concurrency,
		})
		c.Set(string(ContextKeyUserRole), apiKey.User.Role)
		setGroupContext(c, apiKey.Group)
		_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)

		c.Next()
	}
}

// GetAPIKeyFromContext 从上下文中获取API key
func GetAPIKeyFromContext(c *gin.Context) (*service.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*service.APIKey)
	return apiKey, ok
}

// SetOpsFallbackAPIKey 记录已加载的 API Key，供 Ops 错误日志在鉴权早退时回退使用。
// 与 ContextKeyAPIKey 区分：写入它不代表请求已通过鉴权，因此不影响 handler、
// 审计日志等对“已鉴权”的判断。
func SetOpsFallbackAPIKey(c *gin.Context, apiKey *service.APIKey) {
	if c == nil || apiKey == nil {
		return
	}
	c.Set(string(ContextKeyOpsFallbackAPIKey), apiKey)
}

// GetOpsFallbackAPIKey 读取 Ops 错误日志专用的回退 API Key。
func GetOpsFallbackAPIKey(c *gin.Context) (*service.APIKey, bool) {
	value, exists := c.Get(string(ContextKeyOpsFallbackAPIKey))
	if !exists {
		return nil, false
	}
	apiKey, ok := value.(*service.APIKey)
	return apiKey, ok
}

// GetSubscriptionFromContext 从上下文中获取订阅信息
func GetSubscriptionFromContext(c *gin.Context) (*service.UserSubscription, bool) {
	value, exists := c.Get(string(ContextKeySubscription))
	if !exists {
		return nil, false
	}
	subscription, ok := value.(*service.UserSubscription)
	return subscription, ok
}

func setGroupContext(c *gin.Context, group *service.Group) {
	if !service.IsGroupContextValid(group) {
		return
	}
	if existing, ok := c.Request.Context().Value(ctxkey.Group).(*service.Group); ok && existing != nil && existing.ID == group.ID && service.IsGroupContextValid(existing) {
		return
	}
	ctx := context.WithValue(c.Request.Context(), ctxkey.Group, group)
	c.Request = c.Request.WithContext(ctx)
}

func abortIfAPIKeyGroupUnavailable(c *gin.Context, apiKey *service.APIKey) bool {
	code, message, ok := validateAPIKeyGroupAvailable(apiKey)
	if ok {
		return false
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable)
	AbortWithError(c, 403, code, message)
	return true
}

func abortAPIKeyResolutionError(c *gin.Context, err error, google bool) {
	switch {
	case errors.Is(err, service.ErrAPIKeyNotFound):
		writeAPIKeyAuthError(c, http.StatusUnauthorized, "INVALID_API_KEY", "Invalid API key", google)
	case errors.Is(err, service.ErrOrganizationKeyAttributionInvalid),
		errors.Is(err, service.ErrOrganizationMembershipInconsistent):
		writeAPIKeyAuthError(c, http.StatusForbidden, "ORGANIZATION_KEY_ATTRIBUTION_INVALID", "organization key attribution is invalid", google)
	case errors.Is(err, service.ErrOrganizationUnavailable):
		writeAPIKeyAuthError(c, http.StatusForbidden, "ORGANIZATION_UNAVAILABLE", "organization is unavailable", google)
	case errors.Is(err, service.ErrOrganizationKeyAuthorizationUnavailable):
		writeAPIKeyAuthError(c, http.StatusServiceUnavailable, "ORGANIZATION_KEY_AUTHORIZATION_UNAVAILABLE", "organization authorization is temporarily unavailable", google)
	default:
		writeAPIKeyAuthError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to validate API key", google)
	}
}

func abortIfOrganizationKeyAdmissionDenied(
	c *gin.Context,
	apiKey *service.APIKey,
	registry *service.OrganizationSpendingCapabilityRegistry,
	google bool,
) bool {
	if !hasOrganizationKeyAttribution(apiKey) {
		return false
	}
	if ensureOrganizationKeyAuthorizationContext(c, apiKey, google) {
		return true
	}
	authorization := apiKey.OrganizationAuthorization

	if authorization.OrganizationStatus != service.OrganizationStatusActive {
		writeAPIKeyAuthError(c, http.StatusForbidden, "ORGANIZATION_UNAVAILABLE", "organization is unavailable", google)
		return true
	}
	if isOrganizationNonBillableRequest(c) {
		if err := requireOrganizationNonBillableCapabilities(c, registry); err != nil {
			message := infraerrors.Message(err)
			if infraerrors.Reason(err) == "ORGANIZATION_SPENDING_DISABLED" {
				message = "organization spending is not enabled"
			}
			writeAPIKeyAuthError(c, infraerrors.Code(err), infraerrors.Reason(err), message, google)
			return true
		}
		if !organizationNonBillableGroupAllowed(c, apiKey) {
			writeAPIKeyAuthError(c, http.StatusBadRequest, "ORGANIZATION_SPENDING_UNSUPPORTED_REQUEST", "organization spending does not support this endpoint route", google)
			return true
		}
		if abortIfAPIKeyBillingStateInvalid(c, apiKey, google) {
			return true
		}
		return false
	}
	if !google && isOrganizationResponsesHTTPPost(c) {
		return false
	}
	if abortIfAPIKeyBillingStateInvalid(c, apiKey, google) {
		return true
	}
	if err := requireOrganizationSpendingCapabilities(c, registry); err != nil {
		message := infraerrors.Message(err)
		if infraerrors.Reason(err) == "ORGANIZATION_SPENDING_DISABLED" {
			message = "organization spending is not enabled"
		}
		writeAPIKeyAuthError(
			c,
			infraerrors.Code(err),
			infraerrors.Reason(err),
			message,
			google,
		)
		return true
	}
	if !organizationSpendingGroupAllowed(c, apiKey) {
		writeAPIKeyAuthError(c, http.StatusBadRequest, "ORGANIZATION_SPENDING_UNSUPPORTED_REQUEST", "organization spending does not support this endpoint route", google)
		return true
	}
	return false
}

func organizationSpendingGroupAllowed(c *gin.Context, apiKey *service.APIKey) bool {
	if apiKey == nil || apiKey.Group == nil {
		return false
	}
	path := ""
	method := ""
	if c != nil && c.Request != nil {
		method = c.Request.Method
		if c.Request.URL != nil {
			path = c.Request.URL.Path
		}
	}
	if method == http.MethodPost && path == "/v1/messages" {
		return apiKey.Group.Platform == service.PlatformOpenAI ||
			apiKey.Group.Platform == service.PlatformAnthropic ||
			apiKey.Group.Platform == service.PlatformGemini
	}
	return apiKey.Group.Platform == service.PlatformOpenAI
}

func hasOrganizationKeyAttribution(apiKey *service.APIKey) bool {
	if apiKey == nil {
		return false
	}
	if apiKey.OrganizationID != nil || apiKey.OrganizationAuthorization != nil {
		return true
	}
	return apiKey.User != nil && (apiKey.User.OrganizationID != nil || apiKey.User.OrganizationRole != nil)
}

func ensureOrganizationKeyAuthorizationContext(c *gin.Context, apiKey *service.APIKey, google bool) bool {
	if !hasOrganizationKeyAttribution(apiKey) {
		return false
	}
	authorization := apiKey.OrganizationAuthorization
	if !validOrganizationKeyAuthorization(apiKey, authorization) {
		service.ReportOrganizationKeyAttributionAlert("middleware_snapshot_invalid", authorization)
		writeAPIKeyAuthError(c, http.StatusForbidden, "ORGANIZATION_KEY_ATTRIBUTION_INVALID", "organization key attribution is invalid", google)
		return true
	}
	setOrganizationKeyAuthorizationContext(c, authorization)
	return false
}

func validOrganizationKeyAuthorization(apiKey *service.APIKey, authorization *service.OrganizationKeyAuthorization) bool {
	if apiKey == nil || apiKey.User == nil || authorization == nil ||
		apiKey.OrganizationID == nil || apiKey.User.OrganizationID == nil || apiKey.User.OrganizationRole == nil {
		return false
	}
	if *apiKey.User.OrganizationRole != service.OrganizationRoleAdmin &&
		*apiKey.User.OrganizationRole != service.OrganizationRoleMember {
		return false
	}
	return authorization.APIKeyID == apiKey.ID &&
		authorization.UserID == apiKey.User.ID &&
		authorization.UserID == apiKey.UserID &&
		authorization.OrganizationID > 0 &&
		authorization.OrganizationID == *apiKey.OrganizationID &&
		authorization.OrganizationID == *apiKey.User.OrganizationID &&
		authorization.AllocationID > 0 &&
		authorization.AllocationUserID == authorization.UserID &&
		authorization.AllocationOrganizationID == authorization.OrganizationID &&
		authorization.AllocationStatus == domain.OrganizationAllocationStatusActive
}

func setOrganizationKeyAuthorizationContext(c *gin.Context, authorization *service.OrganizationKeyAuthorization) {
	if c == nil || authorization == nil {
		return
	}
	c.Set(string(ContextKeyOrganizationKeyAuthorization), authorization)
	ctx := context.WithValue(c.Request.Context(), ctxkey.OrganizationKeyAuthorization, authorization)
	c.Request = c.Request.WithContext(ctx)
}

// setOrganizationReportingContext captures only the current organization
// dimension. Organization requests continue through the existing personal
// billing/routing pipeline; this value is consumed by telemetry writers.
func setOrganizationReportingContext(c *gin.Context, apiKey *service.APIKey) {
	if c == nil || apiKey == nil {
		return
	}
	if apiKey.OrganizationAuthorization != nil &&
		apiKey.OrganizationAuthorization.OrganizationStatus != "" &&
		apiKey.OrganizationAuthorization.OrganizationStatus != service.OrganizationStatusActive {
		return
	}
	var organizationID int64
	switch {
	case apiKey.OrganizationID != nil:
		organizationID = *apiKey.OrganizationID
	case apiKey.User != nil && apiKey.User.OrganizationID != nil:
		organizationID = *apiKey.User.OrganizationID
	default:
		return
	}
	if organizationID <= 0 {
		return
	}
	c.Set(string(ContextKeyOrganizationReportingID), organizationID)
	ctx := context.WithValue(c.Request.Context(), ctxkey.OrganizationReportingID, organizationID)
	c.Request = c.Request.WithContext(ctx)
}

// GetOrganizationReportingIDFromContext returns the server-derived nullable
// organization dimension for existing usage/error attribution.
func GetOrganizationReportingIDFromContext(c *gin.Context) (*int64, bool) {
	if c == nil {
		return nil, false
	}
	value, exists := c.Get(string(ContextKeyOrganizationReportingID))
	if !exists {
		return nil, false
	}
	organizationID, ok := value.(int64)
	if !ok || organizationID <= 0 {
		return nil, false
	}
	return &organizationID, true
}

// GetOrganizationKeyAuthorizationFromContext returns only the authoritative
// server-side snapshot established by API Key authentication.
func GetOrganizationKeyAuthorizationFromContext(c *gin.Context) (*service.OrganizationKeyAuthorization, bool) {
	if c == nil {
		return nil, false
	}
	value, exists := c.Get(string(ContextKeyOrganizationKeyAuthorization))
	if !exists {
		return nil, false
	}
	authorization, ok := value.(*service.OrganizationKeyAuthorization)
	return authorization, ok && authorization != nil
}

func abortIfAPIKeyBillingStateInvalid(c *gin.Context, apiKey *service.APIKey, google bool) bool {
	if apiKey == nil {
		return false
	}
	switch apiKey.Status {
	case service.StatusAPIKeyQuotaExhausted:
		writeAPIKeyAuthError(c, http.StatusTooManyRequests, "API_KEY_QUOTA_EXHAUSTED", "API key 额度已用完", google)
		return true
	case service.StatusAPIKeyExpired:
		writeAPIKeyAuthError(c, http.StatusForbidden, "API_KEY_EXPIRED", "API key 已过期", google)
		return true
	}
	if apiKey.IsExpired() {
		writeAPIKeyAuthError(c, http.StatusForbidden, "API_KEY_EXPIRED", "API key 已过期", google)
		return true
	}
	if apiKey.IsQuotaExhausted() {
		writeAPIKeyAuthError(c, http.StatusTooManyRequests, "API_KEY_QUOTA_EXHAUSTED", "API key 额度已用完", google)
		return true
	}
	return false
}

func writeAPIKeyAuthError(c *gin.Context, status int, code, message string, google bool) {
	if google {
		abortWithGoogleError(c, status, message)
		return
	}
	AbortWithError(c, status, code, message)
}

type organizationCapabilityRequest struct {
	family string
	shape  string
}

func requireOrganizationSpendingCapabilities(c *gin.Context, registry *service.OrganizationSpendingCapabilityRegistry) error {
	requests := organizationCapabilityRequests(c)
	if len(requests) == 0 {
		return registry.Require("", "")
	}
	for _, request := range requests {
		if err := registry.Require(request.family, request.shape); err != nil {
			return err
		}
	}
	return nil
}

func requireOrganizationNonBillableCapabilities(c *gin.Context, registry *service.OrganizationSpendingCapabilityRegistry) error {
	requests := organizationNonBillableCapabilityRequests(c)
	if len(requests) == 0 {
		return registry.RequireNonBillable("", "")
	}
	for _, request := range requests {
		if err := registry.RequireNonBillable(request.family, request.shape); err != nil {
			return err
		}
	}
	return nil
}

func isOrganizationNonBillableRequest(c *gin.Context) bool {
	return len(organizationNonBillableCapabilityRequests(c)) > 0
}

func organizationNonBillableCapabilityRequests(c *gin.Context) []organizationCapabilityRequest {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return nil
	}
	path := c.Request.URL.Path
	if c.Request.Method == http.MethodGet && (path == "/v1/models" || path == "/models" || path == "/antigravity/models" || path == "/antigravity/v1/models") {
		return []organizationCapabilityRequest{{family: service.OrganizationEndpointFamilyModels, shape: service.OrganizationRequestShapeHTTP}}
	}
	if c.Request.Method == http.MethodGet && (path == "/v1/usage" || path == "/usage" || path == "/antigravity/v1/usage") {
		return []organizationCapabilityRequest{{family: service.OrganizationEndpointFamilyUsage, shape: service.OrganizationRequestShapeHTTP}}
	}
	if strings.HasSuffix(path, "/messages/count_tokens") && c.Request.Method == http.MethodPost {
		return []organizationCapabilityRequest{{family: service.OrganizationEndpointFamilyTokenCounting, shape: service.OrganizationRequestShapeProbe}}
	}
	if c.Request.Method == http.MethodGet && (path == "/v1beta/models" || strings.HasPrefix(path, "/v1beta/models/")) {
		return []organizationCapabilityRequest{{family: service.OrganizationEndpointFamilyModels, shape: service.OrganizationRequestShapeHTTP}}
	}
	if c.Request.Method == http.MethodGet && (path == "/antigravity/v1beta/models" || strings.HasPrefix(path, "/antigravity/v1beta/models/")) {
		return []organizationCapabilityRequest{{family: service.OrganizationEndpointFamilyModels, shape: service.OrganizationRequestShapeHTTP}}
	}
	if c.Request.Method == http.MethodPost && (strings.HasPrefix(path, "/v1beta/models/") || strings.HasPrefix(path, "/antigravity/v1beta/models/")) && isGeminiCountTokensPath(path) {
		return []organizationCapabilityRequest{{family: service.OrganizationEndpointFamilyTokenCounting, shape: service.OrganizationRequestShapeProbe}}
	}
	return nil
}

func isGeminiCountTokensPath(path string) bool {
	return strings.Contains(path, ":countTokens") || strings.Contains(path, "/countTokens")
}

func organizationNonBillableGroupAllowed(c *gin.Context, apiKey *service.APIKey) bool {
	if apiKey == nil || apiKey.Group == nil {
		return false
	}
	path := ""
	if c != nil && c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.Path
	}
	if strings.HasPrefix(path, "/antigravity/") {
		return true
	}
	if strings.HasPrefix(path, "/v1beta/") {
		return apiKey.Group.Platform == service.PlatformGemini
	}
	return true
}

func organizationCapabilityRequests(c *gin.Context) []organizationCapabilityRequest {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return nil
	}
	path := c.Request.URL.Path
	if strings.HasSuffix(path, "/messages/count_tokens") {
		return []organizationCapabilityRequest{{
			family: service.OrganizationEndpointFamilyTokenCounting,
			shape:  service.OrganizationRequestShapeProbe,
		}}
	}
	if path == "/v1/messages" {
		return organizationHTTPOrStreamingCapabilities(service.OrganizationEndpointFamilyMessages)
	}
	if path == "/antigravity/v1/messages" {
		return organizationHTTPOrStreamingCapabilities(service.OrganizationEndpointFamilyAntigravity)
	}
	if isResponsesPath(path) {
		if c.Request.Method == http.MethodGet {
			return []organizationCapabilityRequest{{
				family: service.OrganizationEndpointFamilyResponses,
				shape:  service.OrganizationRequestShapeWebSocketResponseCreate,
			}}
		}
		return organizationHTTPOrStreamingCapabilities(service.OrganizationEndpointFamilyResponses)
	}
	if path == "/v1/chat/completions" || path == "/chat/completions" {
		return organizationHTTPOrStreamingCapabilities(service.OrganizationEndpointFamilyChat)
	}
	if path == "/v1/embeddings" || path == "/embeddings" {
		return []organizationCapabilityRequest{{
			family: service.OrganizationEndpointFamilyEmbeddings,
			shape:  service.OrganizationRequestShapeHTTP,
		}}
	}
	if strings.HasSuffix(path, "/images/generations") {
		return []organizationCapabilityRequest{{
			family: service.OrganizationEndpointFamilyImages,
			shape:  service.OrganizationRequestShapeGenerations,
		}}
	}
	if strings.HasSuffix(path, "/images/edits") {
		return []organizationCapabilityRequest{{
			family: service.OrganizationEndpointFamilyImages,
			shape:  service.OrganizationRequestShapeEdits,
		}}
	}
	if c.Request.Method == http.MethodPost && strings.HasPrefix(path, "/v1beta/models/") {
		return []organizationCapabilityRequest{{
			family: service.OrganizationEndpointFamilyGemini,
			shape:  service.OrganizationRequestShapeModelsAction,
		}}
	}
	if c.Request.Method == http.MethodPost && strings.HasPrefix(path, "/antigravity/v1beta/models/") {
		shape := service.OrganizationRequestShapeHTTP
		if strings.Contains(path, ":streamGenerateContent") {
			shape = service.OrganizationRequestShapeStreaming
		}
		return []organizationCapabilityRequest{{
			family: service.OrganizationEndpointFamilyAntigravity,
			shape:  shape,
		}}
	}
	return nil
}

func organizationHTTPOrStreamingCapabilities(family string) []organizationCapabilityRequest {
	// Authentication runs before request-body parsing. Requiring both possible
	// shapes is conservative: no body shape can be enabled accidentally through
	// a less restrictive route-level classification.
	return []organizationCapabilityRequest{
		{family: family, shape: service.OrganizationRequestShapeHTTP},
		{family: family, shape: service.OrganizationRequestShapeStreaming},
	}
}

func isResponsesPath(path string) bool {
	return path == "/v1/responses" ||
		strings.HasPrefix(path, "/v1/responses/") ||
		path == "/responses" ||
		strings.HasPrefix(path, "/responses/") ||
		path == "/backend-api/codex/responses" ||
		strings.HasPrefix(path, "/backend-api/codex/responses/")
}

func isOrganizationResponsesHTTPPost(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.Method != http.MethodPost {
		return false
	}
	path := strings.TrimRight(c.Request.URL.Path, "/")
	return isResponsesPath(path)
}

func abortIfAPIKeyGroupNotAllowed(c *gin.Context, apiKey *service.APIKey) bool {
	if validateAPIKeyGroupAllowed(apiKey) {
		return false
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable)
	AbortWithError(c, 403, "GROUP_NOT_ALLOWED", "API Key 所属专属分组不再允许当前用户使用")
	return true
}

func validateAPIKeyGroupAllowed(apiKey *service.APIKey) bool {
	if apiKey == nil || apiKey.GroupID == nil || apiKey.User == nil || apiKey.Group == nil {
		return true
	}
	group := apiKey.Group
	if group.IsSubscriptionType() {
		return true
	}
	return apiKey.User.CanBindGroup(group.ID, group.IsExclusive)
}

func validateAPIKeyGroupAvailable(apiKey *service.APIKey) (string, string, bool) {
	if apiKey == nil || apiKey.GroupID == nil {
		return "", "", true
	}
	group := apiKey.Group
	if group == nil || strings.EqualFold(group.Status, "deleted") {
		return "GROUP_DELETED", "API Key 所属分组已删除", false
	}
	if !group.IsActive() {
		return "GROUP_DISABLED", "API Key 所属分组已停用", false
	}
	return "", "", true
}
