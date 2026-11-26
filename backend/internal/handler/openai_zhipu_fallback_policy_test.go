package handler

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestZhipuFallbackUpstreamFailure(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  *service.UpstreamFailoverError
		want bool
	}{
		{"no upstream attempt", nil, false},
		{"rate limited", &service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}, true},
		{"unavailable", &service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}, true},
		{"gateway failure", &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway}, true},
		{"bad request", &service.UpstreamFailoverError{StatusCode: http.StatusBadRequest}, false},
		{"account authentication", &service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable, Stage: service.GatewayFailureStageAccountAuth}, false},
		{"explicitly non-retryable", &service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests, NextAccountAction: service.NextAccountStop}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, zhipuFallbackUpstreamFailure(tt.err))
		})
	}
}

func TestZhipuFallbackScope(t *testing.T) {
	groupID := int64(10)
	key := &service.APIKey{ID: 3, GroupID: &groupID, Group: &service.Group{Platform: service.PlatformZhipu}, User: &service.User{ID: 5}}
	cfg := &config.Config{}
	h := &OpenAIGatewayHandler{cfg: cfg}
	body := []byte(`{"model":"glm-5.3","input":"hello"}`)
	require.Nil(t, h.zhipuFallback(key, nil, "glm-5.3", body))
	cfg.Gateway.ZhipuDeepSeekFallback = config.GatewayZhipuDeepSeekFallbackConfig{
		Enabled: true, SourceGroupID: groupID, SourceKeyID: key.ID, TargetGroupID: 11, TargetModel: "deepseek-v4-pro",
	}
	require.NotNil(t, h.zhipuFallback(key, nil, "glm-5.3", body))
	require.NotNil(t, h.zhipuFallback(key, nil, "glm-5.3-flash", body), "all Zhipu models are covered")
	require.Nil(t, h.zhipuFallback(key, &service.UserSubscription{}, "glm-5.3", body))
	for _, body := range []string{
		`{"previous_response_id":"resp_previous"}`,
		`{"conversation":{"id":"conv_previous"}}`,
		`{"background":true}`,
		`{"input":[{"type":"reasoning","encrypted_content":"provider-owned"}]}`,
		`{"input":[{"type":"item_reference","id":"item_previous"}]}`,
	} {
		require.Nil(t, h.zhipuFallback(key, nil, "glm-5.3", []byte(body)), body)
	}
	otherKey := *key
	otherKey.ID++
	require.Nil(t, h.zhipuFallback(&otherKey, nil, "glm-5.3", body))
	otherKey.ID, otherKey.Group = key.ID, &service.Group{Platform: service.PlatformDeepseek}
	require.Nil(t, h.zhipuFallback(&otherKey, nil, "glm-5.3", body))
	cfg.RunMode = config.RunModeSimple
	require.Nil(t, h.zhipuFallback(key, nil, "glm-5.3", body))
}
