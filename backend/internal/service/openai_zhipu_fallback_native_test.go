//go:build unit

package service

// 智谱 -> DeepSeek 回退在国产供应商原生 Anthropic 路径上的 service 侧前置条件回归测试。
//
// handler 只对灰度命中的智谱主请求写入 WithZhipuFallbackFirstOutput 标记，并以
// c.Writer.Written() 判定能否回退，因此 service 侧在首个语义输出前不得向客户端写任何
// 字节。本文件用真实 native 夹具（nativeAnthropicGLMTestAccount + httpUpstreamRecorder）
// 走完整 forward 入口并读取实际输出：
//   - 流内 event:error 限流/过载（首语义输出前）→ typed failover，零字节写出；
//   - 首语义输出 deadline（openai_first_output_timeout_seconds 非 0 才有）→ typed 504；
//   - 无标记 / 非智谱账号 / 非限流过载错误 / 首输出之后 → 行为不变。

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// zhipuFallbackVolatileItemID 匹配 Responses 转换随机生成的 item id。
var zhipuFallbackVolatileItemID = regexp.MustCompile(`item_[0-9a-f]{24}`)

const (
	zhipuFallbackMessageStartFrame = "event: message_start\n" +
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"glm-5.3","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":1}}}` + "\n\n"
	zhipuFallbackPingFrame    = "event: ping\ndata: {\"type\":\"ping\"}\n\n"
	zhipuFallbackContentStart = "event: content_block_start\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n"
	zhipuFallbackTextDeltaFrame = "event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"pong"}}` + "\n\n"
	zhipuFallbackContentStopFrame  = "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
	zhipuFallbackMessageDeltaFrame = "event: message_delta\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":16}}` + "\n\n"
	zhipuFallbackMessageStopFrame = "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	zhipuFallbackRateLimitError   = `{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit reached for requests"}}`
	zhipuFallbackOverloadedError  = `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`
	zhipuFallbackInvalidReqError  = `{"type":"error","error":{"type":"invalid_request_error","message":"messages: field required"}}`
	zhipuFallbackPermissionError  = `{"type":"error","error":{"type":"permission_error","message":"blocked by safety policy"}}`
	zhipuFallbackZhipu1302Error   = `{"type":"error","error":{"code":"1302","message":"您的账户已达到速率限制，请您控制请求频率"}}`
	zhipuFallbackZhipu1305Error   = `{"type":"error","error":{"code":1305,"message":"该模型当前访问量过大，请您稍后再试"}}`
	zhipuFallbackZhipu1309Error   = `{"type":"error","error":{"code":"1309","message":"您的 GLM Coding Plan 套餐已到期"}}`
	zhipuFallbackHealthyPreamble  = zhipuFallbackMessageStartFrame + zhipuFallbackPingFrame
	zhipuFallbackHealthyRemaining = zhipuFallbackContentStart + zhipuFallbackTextDeltaFrame + zhipuFallbackContentStopFrame +
		zhipuFallbackMessageDeltaFrame + zhipuFallbackMessageStopFrame
)

func zhipuFallbackErrorFrame(data string) string {
	return "event: error\ndata: " + data + "\n\n"
}

type zhipuFallbackIngress struct {
	name    string
	path    string
	body    string
	forward func(svc *OpenAIGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error)
}

// zhipuFallbackIngresses 列出会落到国产供应商原生 Anthropic 端点的三个入站协议。
func zhipuFallbackIngresses(stream bool) []zhipuFallbackIngress {
	return []zhipuFallbackIngress{
		{
			name: "messages",
			path: "/v1/messages",
			body: `{"model":"glm-5.3","max_tokens":64,"stream":` + map[bool]string{true: "true", false: "false"}[stream] +
				`,"messages":[{"role":"user","content":"hi"}]}`,
			forward: func(svc *OpenAIGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
			},
		},
		{
			name: "chat_completions",
			path: "/v1/chat/completions",
			body: `{"model":"glm-5.3","messages":[{"role":"user","content":"hi"}],"stream":` + map[bool]string{true: "true", false: "false"}[stream] + `}`,
			forward: func(svc *OpenAIGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
			},
		},
		{
			name: "responses",
			path: "/v1/responses",
			body: `{"model":"glm-5.3","input":"hi","max_output_tokens":64,"stream":` + map[bool]string{true: "true", false: "false"}[stream] + `}`,
			forward: func(svc *OpenAIGatewayService, ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
				return svc.Forward(ctx, c, account, body)
			},
		},
	}
}

func zhipuFallbackTestService(upstream HTTPUpstream, mutate func(*config.Config)) *OpenAIGatewayService {
	cfg := rawChatCompletionsTestConfig()
	if mutate != nil {
		mutate(cfg)
	}
	return &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
}

func zhipuFallbackTestContext(path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, rec
}

func zhipuFallbackSSEResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type zhipuFallbackWriteStep struct {
	data string
	wait time.Duration // 写出 data 后再等待
}

// zhipuFallbackScriptedResponse 构造按脚本分段到达、最后挂住的上游 SSE 响应，并跟踪 body 关闭。
func zhipuFallbackScriptedResponse(t *testing.T, steps ...zhipuFallbackWriteStep) (*http.Response, *firstOutputCloseTrackingBody) {
	t.Helper()
	pr, pw := io.Pipe()
	release := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer func() { _ = pw.Close() }()
		for _, step := range steps {
			if step.data != "" {
				if _, err := pw.Write([]byte(step.data)); err != nil {
					return
				}
			}
			select {
			case <-time.After(step.wait):
			case <-release:
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = pr.Close()
		close(release)
		<-writerDone
	})
	body := &firstOutputCloseTrackingBody{ReadCloser: pr, closed: make(chan struct{})}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}, body
}

// zhipuFallbackCompatible 断言 failover 错误满足 handler 的回退判定（zhipuFallbackUpstreamFailure）：
// 可换号、非凭据失败、非请求级作用域，且状态码在限流/过载/超时集合内。
func zhipuFallbackCompatible(t *testing.T, err error) *UpstreamFailoverError {
	t.Helper()
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.True(t, failoverErr.ShouldRetryNextAccount())
	require.False(t, failoverErr.IsCredentialFailure())
	require.NotEqual(t, GatewayFailureScopeRequest, failoverErr.Scope)
	switch failoverErr.StatusCode {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
	default:
		t.Fatalf("unexpected fallback status %d", failoverErr.StatusCode)
	}
	return failoverErr
}

func zhipuFallbackOpsKinds(c *gin.Context) []string {
	raw, ok := c.Get(OpsUpstreamErrorsKey)
	if !ok {
		return nil
	}
	events, _ := raw.([]*OpsUpstreamErrorEvent)
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		if event != nil {
			kinds = append(kinds, event.Kind)
		}
	}
	return kinds
}

func TestZhipuFallbackFirstOutputMarkerAndGate(t *testing.T) {
	require.False(t, zhipuFallbackFirstOutputFromContext(context.Background()))
	require.True(t, zhipuFallbackFirstOutputFromContext(WithZhipuFallbackFirstOutput(context.Background())))
	require.False(t, zhipuFallbackFirstOutputFromContext(nil), "nil ctx must stay unguarded")
	require.NotNil(t, WithZhipuFallbackFirstOutput(nil))

	zhipu := nativeAnthropicGLMTestAccount()
	require.False(t, zhipuFallbackFirstOutputGuarded(context.Background(), zhipu))
	require.False(t, zhipuFallbackFirstOutputGuarded(WithZhipuFallbackFirstOutput(context.Background()), nativeAnthropicTestAccount()),
		"非智谱账号即使带标记也不启用")
	require.False(t, zhipuFallbackFirstOutputGuarded(WithZhipuFallbackFirstOutput(context.Background()), nil))
	require.True(t, zhipuFallbackFirstOutputGuarded(WithZhipuFallbackFirstOutput(context.Background()), zhipu))
}

// 无标记 / 非智谱账号：流内 error 帧维持既有行为，绝不产生 failover 错误。
func TestZhipuFallbackInStreamErrorWithoutGuardIsUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ingress := range zhipuFallbackIngresses(true) {
		for _, tc := range []struct {
			name    string
			marked  bool
			account *Account
		}{
			{name: "no marker", account: nativeAnthropicGLMTestAccount()},
			{name: "non zhipu account", marked: true, account: nativeAnthropicTestAccount()},
		} {
			t.Run(ingress.name+"/"+tc.name, func(t *testing.T) {
				sse := zhipuFallbackHealthyPreamble + zhipuFallbackErrorFrame(zhipuFallbackRateLimitError)
				upstream := &httpUpstreamRecorder{resp: zhipuFallbackSSEResponse(sse)}
				svc := zhipuFallbackTestService(upstream, func(cfg *config.Config) {
					cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
				})
				body := []byte(ingress.body)
				c, rec := zhipuFallbackTestContext(ingress.path, body)
				ctx := context.Background()
				if tc.marked {
					ctx = WithZhipuFallbackFirstOutput(ctx)
				}

				result, err := ingress.forward(svc, ctx, c, tc.account, body)

				var failoverErr *UpstreamFailoverError
				require.False(t, errors.As(err, &failoverErr), "must not fail over: %v", err)
				require.True(t, c.Writer.Written(), "upstream frames are relayed as before")
				if ingress.name == "messages" {
					// 直通路径逐字节中继，包括 error 帧。
					require.Equal(t, sse, rec.Body.String())
					require.ErrorContains(t, err, "missing terminal event")
				}
				_ = result
			})
		}
	}
}

// 首语义输出之后才出现的限流 error 帧不属于回退范围：流已经开始，维持既有行为。
func TestZhipuFallbackInStreamErrorAfterOutputIsNotFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ingress := range zhipuFallbackIngresses(true) {
		t.Run(ingress.name, func(t *testing.T) {
			sse := zhipuFallbackHealthyPreamble + zhipuFallbackContentStart + zhipuFallbackTextDeltaFrame +
				zhipuFallbackErrorFrame(zhipuFallbackRateLimitError)
			upstream := &httpUpstreamRecorder{resp: zhipuFallbackSSEResponse(sse)}
			svc := zhipuFallbackTestService(upstream, func(cfg *config.Config) {
				cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
			})
			body := []byte(ingress.body)
			c, rec := zhipuFallbackTestContext(ingress.path, body)

			_, err := ingress.forward(svc, WithZhipuFallbackFirstOutput(context.Background()), c, nativeAnthropicGLMTestAccount(), body)

			var failoverErr *UpstreamFailoverError
			require.False(t, errors.As(err, &failoverErr), "error after first output must not fail over: %v", err)
			require.True(t, c.Writer.Written())
			if ingress.name == "messages" {
				// 直通路径逐字节中继，包括首输出之后的 error 帧。
				require.Equal(t, sse, rec.Body.String())
			}
		})
	}
}

// 非限流/过载类 error 帧（参数、策略、套餐到期）不触发回退。
func TestZhipuFallbackNonRateOverloadErrorIsNotFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ingress := range zhipuFallbackIngresses(true) {
		for _, payload := range []string{zhipuFallbackInvalidReqError, zhipuFallbackPermissionError, zhipuFallbackZhipu1309Error} {
			t.Run(ingress.name+"/"+payload[:40], func(t *testing.T) {
				sse := zhipuFallbackHealthyPreamble + zhipuFallbackErrorFrame(payload)
				upstream := &httpUpstreamRecorder{resp: zhipuFallbackSSEResponse(sse)}
				svc := zhipuFallbackTestService(upstream, nil)
				body := []byte(ingress.body)
				c, _ := zhipuFallbackTestContext(ingress.path, body)

				_, err := ingress.forward(svc, WithZhipuFallbackFirstOutput(context.Background()), c, nativeAnthropicGLMTestAccount(), body)

				var failoverErr *UpstreamFailoverError
				require.False(t, errors.As(err, &failoverErr), "must not fail over: %v", err)
				require.True(t, c.Writer.Written(), "error frame is relayed as before")
			})
		}
	}
}

// zhipuFallbackDelayedUpstream 让上游延迟返回响应头，用于验证 deadline 锚定在请求起点。
type zhipuFallbackDelayedUpstream struct {
	inner HTTPUpstream
	delay time.Duration
}

func (u *zhipuFallbackDelayedUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	time.Sleep(u.delay)
	return u.inner.Do(req, proxyURL, accountID, accountConcurrency)
}

func (u *zhipuFallbackDelayedUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestZhipuFallbackRateOverloadErrorEventClassification(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
		want    bool
	}{
		{"anthropic rate limit", `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, true},
		{"anthropic rate limit without envelope type", `{"error":{"type":"rate_limit_error","message":"slow down"}}`, true},
		{"anthropic overloaded", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, true},
		{"zhipu 1302 rate limit", `{"type":"error","error":{"code":"1302","message":"您的账户已达到速率限制，请您控制请求频率"}}`, true},
		{"zhipu 1305 platform overload numeric code", `{"type":"error","error":{"code":1305,"message":"该模型当前访问量过大，请您稍后再试"}}`, true},
		{"zhipu 1302 bare body", `{"code":"1302","message":"Rate limit reached for requests"}`, true},
		{"explicit status 429", `{"type":"error","error":{"status_code":429,"message":"busy"}}`, true},
		{"explicit status 529", `{"type":"error","error":{"status":529,"message":"overloaded"}}`, true},
		{"openai capacity shed code", `{"type":"error","error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`, true},

		{"invalid request", `{"type":"error","error":{"type":"invalid_request_error","message":"messages: field required"}}`, false},
		{"permission policy", `{"type":"error","error":{"type":"permission_error","message":"blocked by safety policy"}}`, false},
		{"authentication", `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`, false},
		{"not found model", `{"type":"error","error":{"type":"not_found_error","message":"model not found"}}`, false},
		{"generic api error", `{"type":"error","error":{"type":"api_error","message":"Internal server error"}}`, false},
		{"zhipu 1301 content safety", `{"type":"error","error":{"code":"1301","message":"系统检测到输入或生成内容可能包含不安全或敏感内容"}}`, false},
		{"zhipu 1113 arrears", `{"type":"error","error":{"code":"1113","message":"您的账户已欠费，请充值后重试"}}`, false},
		{"zhipu 1308 usage cap", `{"type":"error","error":{"code":"1308","message":"已达到 5 小时的使用上限"}}`, false},
		{"zhipu 1309 plan expired", `{"type":"error","error":{"code":"1309","message":"您的 GLM Coding Plan 套餐已到期"}}`, false},
		{"zhipu 1210 parameter", `{"type":"error","error":{"code":"1210","message":"API 调用参数有误，请检查文档"}}`, false},
		{"cyber policy disguised as rate limit", `{"type":"error","error":{"type":"rate_limit_error","code":"cyber_policy","message":"rate limit exceeded by cyber policy"}}`, false},
		{"cyber policy with 1302", `{"type":"error","error":{"code":"cyber_policy","status_code":429,"message":"blocked"}}`, false},
		{"message_start is not an error", `{"type":"message_start","message":{"id":"msg_1"}}`, false},
		{"not json", `upstream busy`, false},
		{"empty", ``, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, zhipuFallbackRateOverloadErrorEvent([]byte(tc.payload)))
		})
	}
}

func TestZhipuFallbackAnthropicDataStartsOutput(t *testing.T) {
	for _, tc := range []struct {
		name      string
		eventName string
		data      string
		want      bool
	}{
		{"message_start", "message_start", `{"type":"message_start","message":{"id":"msg_1"}}`, false},
		{"ping", "ping", `{"type":"ping"}`, false},
		{"message_start without event line", "", `{"type":"message_start","message":{"id":"msg_1"}}`, false},
		{"content_block_start text", "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`, true},
		{"content_block_start thinking", "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`, true},
		{"content_block_start tool_use", "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}}`, true},
		{"content_block_delta", "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}`, true},
		{"content_block_stop", "content_block_stop", `{"type":"content_block_stop","index":0}`, true},
		{"message_delta", "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`, true},
		{"message_stop", "message_stop", `{"type":"message_stop"}`, true},
		{"non rate error is relayed", "error", `{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`, true},
		{"unknown event type", "content_block_retry", `{"type":"content_block_retry","index":0}`, true},
		{"unknown data without type", "", `{"foo":"bar"}`, true},
		{"done sentinel", "", `[DONE]`, true},
		{"not json", "", `upstream busy`, true},
		{"empty data", "", ``, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, zhipuFallbackAnthropicDataStartsOutput(tc.eventName, tc.data))
		})
	}
}

// 首语义输出前的流内限流/过载 error → typed failover，且客户端零字节（handler 才能换号）。
func TestZhipuFallbackInStreamRateOverloadBeforeOutputFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ingress := range zhipuFallbackIngresses(true) {
		for _, tc := range []struct {
			name         string
			payload      string
			timeout      int
			wantStatuses []int
		}{
			{name: "rate_limit_error", payload: zhipuFallbackRateLimitError, timeout: 1, wantStatuses: []int{http.StatusTooManyRequests}},
			{name: "rate_limit_error without deadline config", payload: zhipuFallbackRateLimitError, timeout: 0, wantStatuses: []int{http.StatusTooManyRequests}},
			{name: "overloaded_error", payload: zhipuFallbackOverloadedError, timeout: 1, wantStatuses: []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusBadGateway, 529}},
			{name: "zhipu 1302", payload: zhipuFallbackZhipu1302Error, timeout: 1, wantStatuses: []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusBadGateway, 529}},
			{name: "zhipu 1305", payload: zhipuFallbackZhipu1305Error, timeout: 1, wantStatuses: []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusBadGateway, 529}},
		} {
			t.Run(ingress.name+"/"+tc.name, func(t *testing.T) {
				sse := zhipuFallbackHealthyPreamble + zhipuFallbackErrorFrame(tc.payload)
				upstream := &httpUpstreamRecorder{resp: zhipuFallbackSSEResponse(sse)}
				svc := zhipuFallbackTestService(upstream, func(cfg *config.Config) {
					cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = tc.timeout
				})
				body := []byte(ingress.body)
				c, rec := zhipuFallbackTestContext(ingress.path, body)

				result, err := ingress.forward(svc, WithZhipuFallbackFirstOutput(context.Background()), c, nativeAnthropicGLMTestAccount(), body)

				failoverErr := zhipuFallbackCompatible(t, err)
				require.Nil(t, result, "failover errors carry no usage result")
				require.Contains(t, tc.wantStatuses, failoverErr.StatusCode)
				require.False(t, c.Writer.Written(), "handler gates the fallback on Writer.Written()")
				require.Equal(t, -1, c.Writer.Size())
				require.Empty(t, rec.Body.String())
				require.Contains(t, zhipuFallbackOpsKinds(c), "failover")
				require.Equal(t, 1, len(upstream.requests), "no in-service replay")
			})
		}
	}
}

// 健康流：受保护与不受保护的输出逐字节一致（首输出前的暂存是透明的）。
// Responses 转换会为每次运行生成随机 item id，比较前先归一化这类易变字段。
func TestZhipuFallbackHealthyStreamIsByteIdentical(t *testing.T) {
	gin.SetMode(gin.TestMode)
	normalize := func(out string) string {
		return zhipuFallbackVolatileItemID.ReplaceAllString(out, "item_<id>")
	}
	for _, ingress := range zhipuFallbackIngresses(true) {
		t.Run(ingress.name, func(t *testing.T) {
			sse := zhipuFallbackHealthyPreamble + zhipuFallbackHealthyRemaining
			run := func(marked bool) (string, error) {
				upstream := &httpUpstreamRecorder{resp: zhipuFallbackSSEResponse(sse)}
				svc := zhipuFallbackTestService(upstream, func(cfg *config.Config) {
					cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
				})
				body := []byte(ingress.body)
				c, rec := zhipuFallbackTestContext(ingress.path, body)
				ctx := context.Background()
				if marked {
					ctx = WithZhipuFallbackFirstOutput(ctx)
				}
				result, err := ingress.forward(svc, ctx, c, nativeAnthropicGLMTestAccount(), body)
				require.NotNil(t, result)
				require.True(t, c.Writer.Written())
				return normalize(rec.Body.String()), err
			}

			plainBody, plainErr := run(false)
			guardedBody, guardedErr := run(true)

			require.NoError(t, plainErr)
			require.NoError(t, guardedErr)
			require.NotEmpty(t, plainBody)
			require.Equal(t, plainBody, guardedBody)
			if ingress.name == "messages" {
				require.Equal(t, sse, guardedBody, "passthrough must stay byte-exact")
			}
		})
	}
}

// 首语义输出前上游迟迟不出内容：网关自己的 keepalive ping 也不得写出。
func TestZhipuFallbackWritesNothingBeforeFirstOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ingress := range zhipuFallbackIngresses(true) {
		for _, marked := range []bool{false, true} {
			t.Run(ingress.name+map[bool]string{false: "/plain", true: "/guarded"}[marked], func(t *testing.T) {
				resp, body := zhipuFallbackScriptedResponse(t,
					zhipuFallbackWriteStep{data: zhipuFallbackHealthyPreamble, wait: 2500 * time.Millisecond},
					zhipuFallbackWriteStep{data: zhipuFallbackHealthyRemaining},
				)
				upstream := &httpUpstreamRecorder{resp: resp}
				svc := zhipuFallbackTestService(upstream, func(cfg *config.Config) {
					cfg.Gateway.StreamKeepaliveInterval = 1
				})
				bodyBytes := []byte(ingress.body)
				c, rec := zhipuFallbackTestContext(ingress.path, bodyBytes)
				ctx := context.Background()
				if marked {
					ctx = WithZhipuFallbackFirstOutput(ctx)
				}

				_, err := ingress.forward(svc, ctx, c, nativeAnthropicGLMTestAccount(), bodyBytes)

				require.NoError(t, err)
				select {
				case <-body.closed:
				default:
					t.Fatal("upstream body must be closed after the stream")
				}
				out := rec.Body.String()
				if ingress.name != "messages" {
					// CC/Responses 原路径本就没有网关心跳，这里只验证流完整。
					require.Contains(t, out, "pong")
					return
				}
				if marked {
					require.Equal(t, zhipuFallbackHealthyPreamble+zhipuFallbackHealthyRemaining, out,
						"guarded stream must not emit gateway pings before the first semantic output")
				} else {
					// 网关自己的 ping 是 `data: {"type": "ping"}`（冒号后带空格），与上游 ping 不同。
					require.Contains(t, out, `data: {"type": "ping"}`, "plain path emits keepalive pings while upstream is slow")
				}
			})
		}
	}
}

// 首语义输出 deadline：上游 HTTP 200 后一直不出语义输出 → typed 504，客户端零字节。
func TestZhipuFallbackFirstOutputDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ingress := range append(zhipuFallbackIngresses(true), zhipuFallbackIngresses(false)[1:]...) {
		for _, preamble := range []string{"", zhipuFallbackHealthyPreamble} {
			t.Run(ingress.name+"/"+map[bool]string{true: "stream", false: "buffered"}[strings.Contains(ingress.body, `"stream":true`)]+"/"+map[bool]string{true: "after_preamble", false: "silent"}[preamble != ""], func(t *testing.T) {
				resp, body := zhipuFallbackScriptedResponse(t, zhipuFallbackWriteStep{data: preamble, wait: 4 * time.Second})
				upstream := &httpUpstreamRecorder{resp: resp}
				svc := zhipuFallbackTestService(upstream, func(cfg *config.Config) {
					cfg.Gateway.ZhipuFallbackFirstOutputTimeoutSeconds = 1
				})
				reqBody := []byte(ingress.body)
				c, rec := zhipuFallbackTestContext(ingress.path, reqBody)

				started := time.Now()
				result, err := ingress.forward(svc, WithZhipuFallbackFirstOutput(context.Background()), c, nativeAnthropicGLMTestAccount(), reqBody)

				require.Less(t, time.Since(started), 2500*time.Millisecond, "deadline must end the wait")
				failoverErr := zhipuFallbackCompatible(t, err)
				require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
				require.True(t, failoverErr.SafeToFailoverAfterWrite)
				require.Contains(t, string(failoverErr.ResponseBody), "first_output_timeout")
				require.Nil(t, result)
				require.False(t, c.Writer.Written())
				require.Empty(t, rec.Body.String())
				require.Contains(t, zhipuFallbackOpsKinds(c), "first_output_timeout")
				select {
				case <-body.closed:
				default:
					t.Fatal("first-output timeout must close the upstream response body")
				}
			})
		}
	}
}

// deadline 锚定在请求起点：等响应头的时间计入，总等待约等于 deadline 而不是 2 倍。
func TestZhipuFallbackDeadlineIncludesResponseHeaderWait(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ingress := range zhipuFallbackIngresses(true) {
		t.Run(ingress.name, func(t *testing.T) {
			resp, _ := zhipuFallbackScriptedResponse(t, zhipuFallbackWriteStep{data: zhipuFallbackHealthyPreamble, wait: 4 * time.Second})
			upstream := &zhipuFallbackDelayedUpstream{inner: &httpUpstreamRecorder{resp: resp}, delay: 600 * time.Millisecond}
			svc := zhipuFallbackTestService(upstream, func(cfg *config.Config) {
				cfg.Gateway.ZhipuFallbackFirstOutputTimeoutSeconds = 1
			})
			reqBody := []byte(ingress.body)
			c, rec := zhipuFallbackTestContext(ingress.path, reqBody)

			started := time.Now()
			_, err := ingress.forward(svc, WithZhipuFallbackFirstOutput(context.Background()), c, nativeAnthropicGLMTestAccount(), reqBody)

			failoverErr := zhipuFallbackCompatible(t, err)
			require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
			require.Less(t, time.Since(started), 1600*time.Millisecond)
			require.False(t, c.Writer.Written())
			require.Empty(t, rec.Body.String())
		})
	}
}

// 上游迟迟不返回响应头：同样受首语义输出 deadline 约束，并取消上游请求。
func TestZhipuFallbackResponseHeaderDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ingress := range zhipuFallbackIngresses(true) {
		t.Run(ingress.name, func(t *testing.T) {
			upstream := &blockingOpenAIResponseHeaderUpstream{canceled: make(chan struct{})}
			svc := zhipuFallbackTestService(upstream, func(cfg *config.Config) {
				cfg.Gateway.ZhipuFallbackFirstOutputTimeoutSeconds = 1
			})
			reqBody := []byte(ingress.body)
			c, rec := zhipuFallbackTestContext(ingress.path, reqBody)

			started := time.Now()
			result, err := ingress.forward(svc, WithZhipuFallbackFirstOutput(context.Background()), c, nativeAnthropicGLMTestAccount(), reqBody)

			require.Less(t, time.Since(started), 1300*time.Millisecond)
			failoverErr := zhipuFallbackCompatible(t, err)
			require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
			require.Contains(t, string(failoverErr.ResponseBody), "first_output_timeout")
			require.Nil(t, result)
			require.False(t, c.Writer.Written())
			require.Empty(t, rec.Body.String())
			select {
			case <-upstream.canceled:
			default:
				t.Fatal("response-header timeout did not cancel the upstream request context")
			}
		})
	}
}

// 未配置 deadline（0）时没有 504：首输出前的空闲仍由既有的读间隔超时处理。
func TestZhipuFallbackDeadlineDisabledKeepsExistingIdleTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ingress := range append(zhipuFallbackIngresses(true), zhipuFallbackIngresses(false)[1:]...) {
		t.Run(ingress.name, func(t *testing.T) {
			resp, _ := zhipuFallbackScriptedResponse(t, zhipuFallbackWriteStep{data: zhipuFallbackHealthyPreamble, wait: 4 * time.Second})
			upstream := &httpUpstreamRecorder{resp: resp}
			svc := zhipuFallbackTestService(upstream, func(cfg *config.Config) {
				cfg.Gateway.StreamDataIntervalTimeout = 1
			})
			reqBody := []byte(ingress.body)
			c, rec := zhipuFallbackTestContext(ingress.path, reqBody)

			started := time.Now()
			_, err := ingress.forward(svc, WithZhipuFallbackFirstOutput(context.Background()), c, nativeAnthropicGLMTestAccount(), reqBody)

			require.Less(t, time.Since(started), 2500*time.Millisecond)
			var failoverErr *UpstreamFailoverError
			require.False(t, errors.As(err, &failoverErr), "idle timeout stays an untyped stream error: %v", err)
			require.ErrorContains(t, err, "stream data interval timeout")
			require.Empty(t, rec.Body.String(), "a preamble without usable output is not committed on idle timeout")
		})
	}
}

// 首语义输出之后 deadline 必须解除：慢流不会被误杀。
func TestZhipuFallbackDeadlineDoesNotCancelStreamAfterFirstOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ingress := range zhipuFallbackIngresses(true) {
		t.Run(ingress.name, func(t *testing.T) {
			resp, body := zhipuFallbackScriptedResponse(t,
				zhipuFallbackWriteStep{data: zhipuFallbackHealthyPreamble + zhipuFallbackContentStart, wait: 1800 * time.Millisecond},
				zhipuFallbackWriteStep{data: zhipuFallbackTextDeltaFrame + zhipuFallbackContentStopFrame + zhipuFallbackMessageDeltaFrame + zhipuFallbackMessageStopFrame},
			)
			upstream := &httpUpstreamRecorder{resp: resp}
			svc := zhipuFallbackTestService(upstream, func(cfg *config.Config) {
				cfg.Gateway.ZhipuFallbackFirstOutputTimeoutSeconds = 1
			})
			reqBody := []byte(ingress.body)
			c, rec := zhipuFallbackTestContext(ingress.path, reqBody)

			result, err := ingress.forward(svc, WithZhipuFallbackFirstOutput(context.Background()), c, nativeAnthropicGLMTestAccount(), reqBody)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, c.Writer.Written())
			require.Contains(t, rec.Body.String(), "pong")
			if ingress.name == "messages" {
				require.Equal(t, zhipuFallbackHealthyPreamble+zhipuFallbackHealthyRemaining, rec.Body.String())
			}
			select {
			case <-body.closed:
			default:
				t.Fatal("upstream body must be closed after the stream")
			}
		})
	}
}

// 健康流结束后 header guard 的 cancel 必须释放（不泄漏 context），无标记时上游 ctx 仍保持分离。
func TestZhipuFallbackHeaderGuardReleasedAfterStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ingress := range zhipuFallbackIngresses(true) {
		for _, marked := range []bool{false, true} {
			t.Run(ingress.name+map[bool]string{false: "/plain", true: "/guarded"}[marked], func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: zhipuFallbackSSEResponse(zhipuFallbackHealthyPreamble + zhipuFallbackHealthyRemaining)}
				svc := zhipuFallbackTestService(upstream, func(cfg *config.Config) {
					cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 5
				})
				reqBody := []byte(ingress.body)
				c, _ := zhipuFallbackTestContext(ingress.path, reqBody)
				ctx := context.Background()
				if marked {
					ctx = WithZhipuFallbackFirstOutput(ctx)
				}

				_, err := ingress.forward(svc, ctx, c, nativeAnthropicGLMTestAccount(), reqBody)

				require.NoError(t, err)
				if marked {
					require.Error(t, upstream.lastReq.Context().Err(), "guarded upstream context must be released with the response body")
				} else {
					require.NoError(t, upstream.lastReq.Context().Err())
				}
			})
		}
	}
}

// 客户端取消语义不变：流式上游请求仍与客户端 ctx 分离，非流式仍随客户端取消。
func TestZhipuFallbackKeepsClientCancellationSemantics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ingress := range zhipuFallbackIngresses(true) {
		t.Run(ingress.name+"/stream stays detached", func(t *testing.T) {
			upstream := &blockingOpenAIResponseHeaderUpstream{canceled: make(chan struct{})}
			svc := zhipuFallbackTestService(upstream, func(cfg *config.Config) {
				cfg.Gateway.ZhipuFallbackFirstOutputTimeoutSeconds = 1
			})
			reqBody := []byte(ingress.body)
			c, _ := zhipuFallbackTestContext(ingress.path, reqBody)
			ctx, cancel := context.WithCancel(WithZhipuFallbackFirstOutput(context.Background()))
			defer cancel()

			done := make(chan error, 1)
			go func() {
				_, err := ingress.forward(svc, ctx, c, nativeAnthropicGLMTestAccount(), reqBody)
				done <- err
			}()
			time.Sleep(150 * time.Millisecond)
			cancel()
			select {
			case <-upstream.canceled:
				t.Fatal("client cancel must not reach the detached streaming upstream request")
			case <-time.After(300 * time.Millisecond):
			}
			select {
			case err := <-done:
				var failoverErr *UpstreamFailoverError
				require.ErrorAs(t, err, &failoverErr, "only the first-output deadline ends the wait")
				require.Equal(t, http.StatusGatewayTimeout, failoverErr.StatusCode)
			case <-time.After(2 * time.Second):
				t.Fatal("forward did not return after the deadline")
			}
		})
	}

	ingress := zhipuFallbackIngresses(false)[0]
	t.Run(ingress.name+"/buffered follows client cancel", func(t *testing.T) {
		upstream := &blockingOpenAIResponseHeaderUpstream{canceled: make(chan struct{})}
		svc := zhipuFallbackTestService(upstream, func(cfg *config.Config) {
			cfg.Gateway.ZhipuFallbackFirstOutputTimeoutSeconds = 1
		})
		reqBody := []byte(ingress.body)
		c, _ := zhipuFallbackTestContext(ingress.path, reqBody)
		ctx, cancel := context.WithCancel(WithZhipuFallbackFirstOutput(context.Background()))

		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()
		_, err := ingress.forward(svc, ctx, c, nativeAnthropicGLMTestAccount(), reqBody)

		select {
		case <-upstream.canceled:
		default:
			t.Fatal("buffered upstream request must follow client cancellation")
		}
		var failoverErr *UpstreamFailoverError
		require.False(t, errors.As(err, &failoverErr), "client cancel stays a plain error: %v", err)
		require.ErrorIs(t, err, context.Canceled)
	})
}
