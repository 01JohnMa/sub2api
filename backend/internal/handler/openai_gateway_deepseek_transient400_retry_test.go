//go:build unit

package handler

// DeepSeek 间歇性 400 同账号重试钩子的 handler 级回归测试（upgrade-v0.2.24）。
//
// 生产定制位于 internal/service/openai_gateway_upstream_errors.go：
//   - isDeepSeekTransientThinkingModeError 识别两种 DeepSeek 间歇 400
//     （response_format 灰度抖动 / thinking 回传校验抖动）；
//   - handleErrorResponse / handleCompatErrorResponse 在确定性透传前把它们
//     归一成 RetryableOnSameAccount=true（SameAccountRetryMax=1，间隔 400ms）；
//   - handler 的池模式同账号重试分支（openai_gateway_handler.go 两处主径）
//     消费该标记在同一账号上静默重试。
//
// 本测试覆盖两条主径：Messages（Anthropic 兼容派发，错误经
// handleCompatErrorResponse）与 Responses（原生转发，错误经 handleErrorResponse）。
// 上游第 1 次返回特征 400、第 2 次返回成功，断言客户端拿到 200 且两次上游
// 请求落在同一账号。钩子若失效，该 400 会被当作确定性客户端错误透传
// （客户端收到 400，仅 1 次上游调用）。
//
// 注意：messages 派发把 Anthropic 请求转成 Responses API 并强制上游流式
// （openai_gateway_messages.go responsesReq.Stream = true），因此 Messages
// 用例的上游成功报文是 Responses SSE 序列，客户端也用 stream:true 以
// Anthropic SSE 形态断言。

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 两种 DeepSeek 间歇 400 的上游特征报文（与钩子匹配串逐一对应）。
const (
	deepSeekResponseFormat400Body   = `{"error":{"message":"Failed to check request body: response_format type is unavailable for this model (gray rollout)","type":"invalid_request_error","param":"","code":"invalid_request_error"}}`
	deepSeekThinkingPassback400Body = "{\"error\":{\"message\":\"Failed to check reasoning field: reasoning_content` in the thinking mode must be passed back in the next request\",\"type\":\"invalid_request_error\",\"param\":\"\",\"code\":\"invalid_request_error\"}}"
)

// deepSeekResponsesSSESuccess 上游第 2 次调用返回的 Responses SSE 成功序列
// （与 openai_gateway_messages_pre_content_failover_test.go 的成功序列同构）。
const deepSeekResponsesSSESuccess = `data: {"type":"response.created","response":{"id":"resp_ds_ok","object":"response","model":"deepseek-chat","status":"in_progress","output":[]}}` + "\n\n" +
	`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}` + "\n\n" +
	`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"ok"}` + "\n\n" +
	`data: {"type":"response.completed","response":{"id":"resp_ds_ok","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n"

const deepSeekRetryTestAccountID = int64(8801)

// deepSeekTransient400Upstream 记录每次上游调用的 accountID：
// 第 1 次返回特征 400，第 2 次起返回 successBody（SSE 用 text/event-stream）。
type deepSeekTransient400Upstream struct {
	service.HTTPUpstream
	mu           sync.Mutex
	hits         []int64
	firstStatus  int
	firstBody    string
	successBody  string
	successIsSSE bool
}

func (u *deepSeekTransient400Upstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	if req.Body != nil {
		_, _ = io.Copy(io.Discard, req.Body)
	}
	u.mu.Lock()
	u.hits = append(u.hits, accountID)
	call := len(u.hits)
	u.mu.Unlock()
	if call == 1 {
		return deepSeekJSONUpstreamResponse(u.firstStatus, u.firstBody), nil
	}
	return deepSeekUpstreamResponse(http.StatusOK, u.successBody, u.successIsSSE), nil
}

func (u *deepSeekTransient400Upstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.hits...)
}

func deepSeekJSONUpstreamResponse(status int, body string) *http.Response {
	return deepSeekUpstreamResponse(status, body, false)
}

func deepSeekUpstreamResponse(status int, body string, sse bool) *http.Response {
	contentType := "application/json"
	if sse {
		contentType = "text/event-stream"
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type deepSeekTransient400TestEnv struct {
	handler  *OpenAIGatewayHandler
	upstream *deepSeekTransient400Upstream
}

// newDeepSeekTransient400TestEnv 搭建完整转发链：单个池模式 OpenAI 兼容账号
// + 注入式 mock 上游。不注入 rateLimitService（nil 安全，钩子前置分支均跳过）。
func newDeepSeekTransient400TestEnv(t *testing.T) *deepSeekTransient400TestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)

	accounts := []service.Account{{
		ID: deepSeekRetryTestAccountID, Name: "deepseek-pool",
		Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials: map[string]any{
			"api_key":   "sk-deepseek-test",
			"base_url":  "https://api.deepseek.test",
			"pool_mode": true,
		},
	}}

	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.MaxAccountSwitches = 3
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true

	repo := &grokCredentialHandlerRepo{accounts: accounts, missingOnGet: map[int64]bool{}}
	upstream := &deepSeekTransient400Upstream{}
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	gateway := service.NewOpenAIGatewayService(
		repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billingCache, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	cache := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(cache), billingCache, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
	return &deepSeekTransient400TestEnv{handler: h, upstream: upstream}
}

func newDeepSeekTransient400TestContext(t *testing.T, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	groupID := int64(8800)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID: 8802, GroupID: &groupID,
		User:  &service.User{ID: 8803, Status: service.StatusActive},
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, AllowMessagesDispatch: true},
	})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 8803, Concurrency: 0})
	return c, rec
}

// Messages 主径（handleCompatErrorResponse）：response_format 灰度 400 → 同账号
// 重试 → 200 Responses SSE → 客户端拿到 Anthropic message_start 流。
func TestOpenAIMessagesDeepSeekTransient400RetriesSameAccountThenSucceeds(t *testing.T) {
	env := newDeepSeekTransient400TestEnv(t)
	env.upstream.firstStatus = http.StatusBadRequest
	env.upstream.firstBody = deepSeekResponseFormat400Body
	env.upstream.successBody = deepSeekResponsesSSESuccess
	env.upstream.successIsSSE = true

	c, rec := newDeepSeekTransient400TestContext(t, "/v1/messages",
		`{"model":"deepseek-chat","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	env.handler.Messages(c)

	hits := env.upstream.calls()
	require.Len(t, hits, 2, "resp=%d body=%s", rec.Code, rec.Body.String())
	require.Equal(t, []int64{deepSeekRetryTestAccountID, deepSeekRetryTestAccountID}, hits,
		"两次上游请求必须落在同一账号（DeepSeek 间歇 400 不得换号）")
	require.Equal(t, http.StatusOK, rec.Code, "客户端必须拿到 200，body=%s", rec.Body.String())
	require.Equal(t, 1, strings.Count(rec.Body.String(), "event: message_start"), "body=%s", rec.Body.String())
	require.Contains(t, rec.Body.String(), `"ok"`)
	require.NotContains(t, rec.Body.String(), "response_format type is unavailable")
}

// Responses 主径（handleErrorResponse）：thinking 回传校验 400 → 同账号重试 →
// 200 Responses 完成态 JSON → 客户端拿到 200。
func TestOpenAIResponsesDeepSeekTransient400RetriesSameAccountThenSucceeds(t *testing.T) {
	env := newDeepSeekTransient400TestEnv(t)
	env.upstream.firstStatus = http.StatusBadRequest
	env.upstream.firstBody = deepSeekThinkingPassback400Body
	env.upstream.successBody = `{"id":"resp_deepseek_ok","object":"response","created_at":1764000000,"status":"completed","model":"deepseek-chat","output":[{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"ok","annotations":[]}]}],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}`

	c, rec := newDeepSeekTransient400TestContext(t, "/openai/v1/responses",
		`{"model":"deepseek-chat","input":"hi","stream":false}`)
	env.handler.Responses(c)

	hits := env.upstream.calls()
	require.Len(t, hits, 2, "resp=%d body=%s", rec.Code, rec.Body.String())
	require.Equal(t, []int64{deepSeekRetryTestAccountID, deepSeekRetryTestAccountID}, hits,
		"两次上游请求必须落在同一账号（DeepSeek 间歇 400 不得换号）")
	require.Equal(t, http.StatusOK, rec.Code, "客户端必须拿到 200，body=%s", rec.Body.String())
	require.Equal(t, "resp_deepseek_ok", gjson.GetBytes(rec.Body.Bytes(), "id").String())
}
