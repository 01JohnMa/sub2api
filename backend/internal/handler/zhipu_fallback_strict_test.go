//go:build unit

package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Sticky pins must only survive a successful target response; every failure
// exit is covered by the deferred finalizer.
func TestZhipuFallbackFinalizeClearsPinOnFailure(t *testing.T) {
	h := &OpenAIGatewayHandler{}
	fallback := &zhipuDeepSeekFallback{rule: fallbackRuleForTest(), used: true}
	key := zhipuStickyKey(fallback.rule.SourceGroupID, 113, "session-a")
	t.Cleanup(func() { h.clearZhipuSticky(key) })

	h.markZhipuSticky(key)
	require.True(t, h.zhipuStickyActive(key))

	succeeded := false
	h.finalizeZhipuFallback(fallback, key, &succeeded)
	require.False(t, h.zhipuStickyActive(key), "failed target response must clear the pin")

	h.markZhipuSticky(key)
	succeeded = true
	h.finalizeZhipuFallback(fallback, key, &succeeded)
	require.True(t, h.zhipuStickyActive(key), "successful target response must keep the pin")

	// Reading an active pin must not extend its lifetime.
	before := h.zhipuSticky.until[key]
	require.True(t, h.zhipuStickyActive(key))
	require.Equal(t, before, h.zhipuSticky.until[key], "read must not refresh the TTL")
}

// An indeterminate pool diagnosis is not evidence of a transient outage.
func TestZhipuPoolExhaustedRequiresDefinitiveDiagnosis(t *testing.T) {
	groupID := int64(117)
	apiKey := &service.APIKey{
		ID:      113,
		GroupID: &groupID,
		Group:   &service.Group{Platform: service.PlatformZhipu},
		User:    &service.User{ID: 1},
	}
	h := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}}
	require.False(t, h.zhipuPoolExhaustedTransient(context.Background(), apiKey, "glm-5.3"))
}

func fallbackRuleForTest() config.GatewayZhipuDeepSeekFallbackConfig {
	return config.GatewayZhipuDeepSeekFallbackConfig{
		Enabled:       true,
		SourceGroupID: 117,
		TargetGroupID: 116,
		TargetModel:   "deepseek-flash",
	}
}
