package handler

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type zhipuDeepSeekFallback struct {
	rule config.GatewayZhipuDeepSeekFallbackConfig
	used bool
}

func (h *OpenAIGatewayHandler) zhipuFallback(apiKey *service.APIKey, subscription *service.UserSubscription, model string, body []byte) *zhipuDeepSeekFallback {
	if h.cfg == nil || h.cfg.RunMode == config.RunModeSimple || subscription != nil || apiKey == nil || apiKey.GroupID == nil || apiKey.Group == nil || apiKey.User == nil ||
		apiKey.Group.Platform != service.PlatformZhipu || apiKey.Group.IsSubscriptionType() {
		return nil
	}
	rule := h.cfg.Gateway.ZhipuDeepSeekFallback
	if !rule.Enabled || *apiKey.GroupID != rule.SourceGroupID {
		return nil
	}
	if rule.SourceKeyID > 0 && apiKey.ID != rule.SourceKeyID {
		return nil
	}
	// Server-held history and background Responses jobs are not portable to another provider.
	for _, field := range []string{"previous_response_id", "conversation", "container"} {
		if v := gjson.GetBytes(body, field); v.Exists() && v.Type != gjson.Null && v.String() != "" {
			return nil
		}
	}
	if gjson.GetBytes(body, "background").Bool() {
		return nil
	}
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("encrypted_content").Exists() || item.Get("type").String() == "item_reference" {
			return nil
		}
	}
	if coverage := service.AnalyzeToolCallOutputContextCoverageBytes(body); coverage.HasFunctionCallOutput && !coverage.ContextCoversAllCallIDs {
		return nil
	}
	return &zhipuDeepSeekFallback{rule: rule}
}

// zhipuPoolExhaustedTransient reports an empty Zhipu pool caused by transient
// account state (rate limit, overload, temporary block) rather than a model
// that no Zhipu account supports.
func (h *OpenAIGatewayHandler) zhipuPoolExhaustedTransient(ctx context.Context, apiKey *service.APIKey, model string) bool {
	if h == nil || h.gatewayService == nil || apiKey == nil || apiKey.GroupID == nil || apiKey.Group == nil ||
		apiKey.Group.Platform != service.PlatformZhipu {
		return false
	}
	result := h.gatewayService.DiagnoseModelAvailabilityForPlatform(ctx, apiKey.GroupID, model, service.PlatformZhipu)
	return result.HasAccountsInPool && result.HasModelSupport
}

const zhipuFallbackStickyTTL = 30 * time.Minute

type zhipuFallbackStickyStore struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func zhipuStickyKey(apiKey *service.APIKey, sessionHash string) string {
	if apiKey == nil || apiKey.GroupID == nil || sessionHash == "" {
		return ""
	}
	return fmt.Sprintf("%d:%s", *apiKey.GroupID, sessionHash)
}

// zhipuStickyActive reports whether this conversation already failed over.
// The TTL slides on every request so a busy conversation stays on the fallback
// provider until it has been idle for the whole TTL.
func (h *OpenAIGatewayHandler) zhipuStickyActive(key string) bool {
	if h == nil || key == "" {
		return false
	}
	h.zhipuStickyOnce.Do(func() {
		h.zhipuSticky = &zhipuFallbackStickyStore{until: make(map[string]time.Time)}
	})
	h.zhipuSticky.mu.Lock()
	defer h.zhipuSticky.mu.Unlock()
	until, ok := h.zhipuSticky.until[key]
	if !ok || time.Now().After(until) {
		delete(h.zhipuSticky.until, key)
		return false
	}
	h.zhipuSticky.until[key] = time.Now().Add(zhipuFallbackStickyTTL)
	return true
}

func (h *OpenAIGatewayHandler) markZhipuSticky(key string) {
	if h == nil || key == "" {
		return
	}
	h.zhipuStickyOnce.Do(func() {
		h.zhipuSticky = &zhipuFallbackStickyStore{until: make(map[string]time.Time)}
	})
	h.zhipuSticky.mu.Lock()
	defer h.zhipuSticky.mu.Unlock()
	now := time.Now()
	for k, until := range h.zhipuSticky.until {
		if now.After(until) {
			delete(h.zhipuSticky.until, k)
		}
	}
	h.zhipuSticky.until[key] = now.Add(zhipuFallbackStickyTTL)
}

func zhipuFallbackUpstreamFailure(err *service.UpstreamFailoverError) bool {
	if err == nil || !err.ShouldRetryNextAccount() || err.IsCredentialFailure() || err.Scope == service.GatewayFailureScopeRequest {
		return false
	}
	switch err.StatusCode {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
		return true
	default:
		return false
	}
}

func (f *zhipuDeepSeekFallback) switched() bool { return f != nil && f.used }

// tryZhipuFallback prepares an attempt-local key view; neither the stored key nor
// the caller identity used by middleware is rebound to the target group.
func (h *OpenAIGatewayHandler) tryZhipuFallback(c *gin.Context, f *zhipuDeepSeekFallback, apiKey *service.APIKey, body []byte, protocol, sessionHash string, sticky bool, err *service.UpstreamFailoverError, releasePrimary func(), reqLog *zap.Logger) (*service.APIKey, service.ChannelMappingResult, string, func()) {
	if f == nil || f.used || c.Writer.Written() || c.Request.Context().Err() != nil {
		return nil, service.ChannelMappingResult{}, "", nil
	}
	if !sticky && !zhipuFallbackUpstreamFailure(err) {
		return nil, service.ChannelMappingResult{}, "", nil
	}
	ctx := c.Request.Context()
	group, resolveErr := h.apiKeyService.ResolveDeepSeekFallbackGroup(ctx, apiKey.User, f.rule.TargetGroupID, f.rule.TargetModel)
	if resolveErr != nil {
		reqLog.Info("openai.zhipu_fallback_skipped", zap.String("reason", "target_not_allowed"))
		return nil, service.ChannelMappingResult{}, "", nil
	}
	targetKey := cloneAPIKeyWithGroup(apiKey, group)
	ctx = context.WithValue(ctx, ctxkey.Group, group)
	mapping, restricted := h.gatewayService.ResolveChannelMappingAndRestrict(ctx, targetKey.GroupID, f.rule.TargetModel)
	if restricted {
		reqLog.Info("openai.zhipu_fallback_skipped", zap.String("reason", "channel_restricted"))
		return nil, service.ChannelMappingResult{}, "", nil
	}
	mapping.MappedModel = openAIChannelForwardModel(mapping, f.rule.TargetModel)
	mapping.Mapped = true
	mapping.BillingModelSource = service.BillingModelSourceUpstream
	estimate := tokenInflightEstimate(f.rule.TargetModel, body)
	if cost, priced := h.gatewayService.EstimateInflightReservation(ctx, targetKey, estimate); !priced || cost <= 0 {
		reqLog.Info("openai.zhipu_fallback_skipped", zap.String("reason", "target_unpriced"))
		return nil, service.ChannelMappingResult{}, "", nil
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		reqLog.Info("openai.zhipu_fallback_skipped", zap.String("reason", "no_auth_subject"))
		return nil, service.ChannelMappingResult{}, "", nil
	}
	targetBody := h.gatewayService.ReplaceModelInBody(body, mapping.MappedModel)
	if decision := h.checkSecurityAuditStage(c, reqLog, targetKey, subject, protocol, mapping.MappedModel, targetBody, "zhipu_deepseek_fallback"); decision != nil && !decision.AllowNextStage {
		reqLog.Info("openai.zhipu_fallback_skipped", zap.String("reason", "security_audit_blocked"))
		return nil, service.ChannelMappingResult{}, "", nil
	}
	// The caller has already passed the user-wide RPM gate. Only charge the
	// target group RPM counter here, without carrying the source group's override.
	admissionKey, admissionUser := *targetKey, *targetKey.User
	admissionUser.RPMLimit = 0
	admissionUser.UserGroupRPMOverride = nil
	admissionKey.User = &admissionUser
	if err := h.billingCacheService.CheckBillingEligibility(ctx, &admissionUser, &admissionKey, group, nil, service.PlatformDeepseek); err != nil {
		reqLog.Info("openai.zhipu_fallback_skipped", zap.String("reason", "admission_failed"))
		return nil, service.ChannelMappingResult{}, "", nil
	}
	releasePrimary()
	ctx, release, reserveErr := reserveInflightBalanceCtx(ctx, h.billingCacheService, h.gatewayService, targetKey, nil, estimate)
	if reserveErr != nil || c.Request.Context().Err() != nil {
		if release != nil {
			release()
		}
		reqLog.Info("openai.zhipu_fallback_skipped", zap.String("reason", "inflight_reserve_failed"))
		return nil, service.ChannelMappingResult{}, "", nil
	}
	c.Request = c.Request.WithContext(ctx)
	f.used = true
	h.markZhipuSticky(zhipuStickyKey(apiKey, sessionHash))
	status := 0
	if err != nil {
		status = err.StatusCode
	}
	reqLog.Info("openai.zhipu_fallback_switching",
		zap.Bool("sticky_reuse", sticky),
		zap.Int64("source_group_id", f.rule.SourceGroupID),
		zap.Int64("target_group_id", f.rule.TargetGroupID),
		zap.String("target_model", mapping.MappedModel),
		zap.Int("upstream_status", status),
	)
	return targetKey, mapping, service.PlatformDeepseek, release
}
