package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

// 只有命中灰度规则的智谱主请求才暂存上游 SSE 前导帧；回退后的 DeepSeek
// 请求保留原来的流式行为。前导帧在首个有效事件到达时原样交给协议适配器。

type zhipuFallbackFirstOutputContextKey struct{}

// WithZhipuFallbackFirstOutput 标记当前请求为智谱 -> DeepSeek 回退的灰度主请求。
func WithZhipuFallbackFirstOutput(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, zhipuFallbackFirstOutputContextKey{}, true)
}

func zhipuFallbackFirstOutputFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, _ := ctx.Value(zhipuFallbackFirstOutputContextKey{}).(bool)
	return enabled
}

// zhipuFallbackFirstOutputGuarded 判定上游请求是否启用首语义输出保护：
// 必须同时满足请求带灰度标记、账号为智谱平台。
func zhipuFallbackFirstOutputGuarded(ctx context.Context, account *Account) bool {
	return account != nil && account.IsZhipu() && zhipuFallbackFirstOutputFromContext(ctx)
}

// zhipuFallbackNativeRateOverloadCodes 是智谱官方文档定义的限流/过载错误码
// （docs.bigmodel.cn/cn/api/rate-limit、docs.bigmodel.cn/cn/faq/api-code）：
// 1302 = 账户已达到速率限制，1305 = 平台服务过载，HTTP 层均为 429。
// 同为 429 的 1113（欠费）、1308（用量上限）、1309（套餐到期）属于账号额度/计费状态，
// 仍交给既有的 429 账号处置，不在此列。
var zhipuFallbackNativeRateOverloadCodes = map[string]struct{}{
	"1302": {},
	"1305": {},
}

// zhipuFallbackRateOverloadErrorEvent 判定流内 error 事件的 data 是否为限流/过载。
// 只认结构化信号（Anthropic 错误类型、显式状态码、上游容量降载码、智谱限流/过载码），
// 状态推导复用 openAIStreamFailedEventSemanticStatus；参数错误、策略/风控拦截、
// 认证与额度类错误一律不算。
func zhipuFallbackRateOverloadErrorEvent(payload []byte) bool {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return false
	}
	// 风控拦截即使披着限流外衣也不是限流（对齐 openAIStreamErrorEventShouldFailover）。
	if hit, _, _ := detectOpenAICyberPolicy(payload); hit {
		return false
	}
	for _, path := range []string{"error.code", "code"} {
		if _, ok := zhipuFallbackNativeRateOverloadCodes[strings.TrimSpace(gjson.GetBytes(payload, path).String())]; ok {
			return true
		}
	}
	switch openAIStreamFailedEventSemanticStatus(payload, extractOpenAISSEErrorMessage(payload)) {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable, 529:
		return true
	}
	// Anthropic 标准过载类型不带状态码，通用分类器会落到 502，这里显式认。
	return strings.EqualFold(strings.TrimSpace(gjson.GetBytes(payload, "error.type").String()), "overloaded_error")
}

// zhipuFallbackAnthropicEventType 返回一条 data 行所属的 Anthropic 事件类型：优先取 data
// JSON 的 type，缺失时回退到同帧 `event:` 行的事件名；都没有则返回空串。
func zhipuFallbackAnthropicEventType(eventName, data string) string {
	trimmed := strings.TrimSpace(data)
	if trimmed != "" && trimmed != "[DONE]" && gjson.Valid(trimmed) {
		if eventType := strings.TrimSpace(gjson.Get(trimmed, "type").String()); eventType != "" {
			return eventType
		}
	}
	return strings.TrimSpace(eventName)
}

// zhipuFallbackAnthropicDataStartsOutput 判定一条 Anthropic SSE data 行是否意味着客户端
// 语义输出已经开始。只有生命周期帧（message_start / ping）不算输出；其余事件、无法识别
// 的 data 以及非限流/过载的 error 帧都算输出开始——保守提交，此后绝不再回退。
func zhipuFallbackAnthropicDataStartsOutput(eventName, data string) bool {
	switch eventType := zhipuFallbackAnthropicEventType(eventName, data); {
	case strings.EqualFold(eventType, "message_start"), strings.EqualFold(eventType, "ping"):
		return false
	default:
		return true
	}
}

var (
	errZhipuFallbackFirstOutputTimeout = errors.New("zhipu first output timeout")
	errZhipuFallbackPreludeIdle        = errors.New("stream data interval timeout")
)

type zhipuFallbackPreludeError struct {
	started time.Time
	timeout time.Duration
	phase   string
	headers http.Header
	payload []byte
}

func (e *zhipuFallbackPreludeError) Error() string {
	if e.timeout > 0 {
		return errZhipuFallbackFirstOutputTimeout.Error()
	}
	return "zhipu stream reported rate limit or overload before output"
}

// doZhipuFallbackUpstream shares the pre-output gate across all three text
// adapters. After readiness, the original bytes and regular stream draining
// are handed back to the caller; the first-output timer never owns the rest.
func (s *OpenAIGatewayService) doZhipuFallbackUpstream(req *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	started := time.Now()
	ctx, cancel := context.WithCancelCause(req.Context())
	timeout := s.zhipuFallbackFirstOutputTimeout()
	var timer *time.Timer
	timeoutCause := errZhipuFallbackFirstOutputTimeout
	if timeout > 0 {
		timer = time.AfterFunc(timeout, func() { cancel(errZhipuFallbackFirstOutputTimeout) })
	}
	resp, err := s.httpUpstream.Do(req.WithContext(ctx), proxyURL, account.ID, account.Concurrency)
	phase := "response_headers"
	var payload []byte
	var body io.ReadCloser
	var bodyReader io.Reader
	var closeOnce sync.Once
	closeBody := func() {
		closeOnce.Do(func() { _ = body.Close() })
	}
	stopClosing := func() bool { return true }
	if resp != nil && resp.Body != nil {
		body = resp.Body
		bodyReader = body
		if err == nil && resp.StatusCode == http.StatusOK && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
			phase = "stream_first_semantic_output"
			stopClosing = context.AfterFunc(ctx, closeBody)
			if timer == nil && s.cfg != nil && s.cfg.Gateway.StreamDataIntervalTimeout > 0 {
				timeoutCause = errZhipuFallbackPreludeIdle
				timer = time.AfterFunc(time.Duration(s.cfg.Gateway.StreamDataIntervalTimeout)*time.Second, func() { cancel(errZhipuFallbackPreludeIdle) })
			}
			bodyReader, payload = readZhipuFallbackPrelude(body)
		}
	}
	if timer != nil && !timer.Stop() {
		cancel(timeoutCause)
	}
	stopClosing()
	cause := context.Cause(ctx)
	if cause != nil || err != nil || len(payload) != 0 {
		if body != nil {
			closeBody()
		}
		cancel(nil)
		var headers http.Header
		if resp != nil {
			headers = resp.Header.Clone()
		}
		switch {
		case errors.Is(cause, errZhipuFallbackFirstOutputTimeout):
			return nil, &zhipuFallbackPreludeError{started: started, timeout: timeout, phase: phase, headers: headers}
		case cause != nil:
			return nil, cause
		case len(payload) != 0:
			return nil, &zhipuFallbackPreludeError{payload: payload, headers: headers}
		default:
			return nil, err
		}
	}
	if body == nil {
		cancel(nil)
		return resp, nil
	}
	resp.Body = &openAIRequestContextReadCloser{
		ReadCloser: zhipuFallbackReplayBody{Reader: bodyReader, Closer: body},
		cleanup:    func() { cancel(nil) },
	}
	return resp, nil
}

type zhipuFallbackReplayBody struct {
	io.Reader
	io.Closer
}

// readZhipuFallbackPrelude is bounded even for malformed upstream SSE. When the
// limit is reached, pass through the complete buffered prefix and stop probing.
func readZhipuFallbackPrelude(body io.Reader) (io.Reader, []byte) {
	reader := bufio.NewReaderSize(body, openAIFirstOutputStageMemoryLimit)
	var prefix bytes.Buffer
	var eventName string
	for prefix.Len() < openAIFirstOutputStageMemoryLimit {
		line, err := reader.ReadSlice('\n')
		prefix.Write(line)
		if err != nil {
			if err != io.EOF && err != bufio.ErrBufferFull {
				return io.MultiReader(&prefix, &zhipuFallbackReadError{err: err}, reader), nil
			}
			break
		}
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			eventName = ""
			continue
		}
		if name, ok := strings.CutPrefix(trimmed, "event:"); ok {
			eventName = strings.TrimSpace(name)
			continue
		}
		data, ok := extractOpenAISSEDataLine(trimmed)
		if !ok {
			continue
		}
		if payload := []byte(data); zhipuFallbackRateOverloadErrorEvent(payload) {
			return nil, payload
		}
		switch event := zhipuFallbackAnthropicEventType(eventName, data); {
		case !zhipuFallbackAnthropicDataStartsOutput(eventName, data):
			continue
		case event != "" && openAIStreamEventIsMetadata(event):
			continue
		}
		break
	}
	return io.MultiReader(&prefix, reader), nil
}

type zhipuFallbackReadError struct{ err error }

func (r *zhipuFallbackReadError) Read([]byte) (int, error) {
	err := r.err
	r.err = io.EOF
	return 0, err
}

// zhipuFallbackFirstOutputTimeout 是智谱回退请求的首语义输出超时；与 OpenAI 平台的
// 全局首输出超时分离，避免改变 GPT 流量的行为。
func (s *OpenAIGatewayService) zhipuFallbackFirstOutputTimeout() time.Duration {
	if s == nil || s.cfg == nil || s.cfg.Gateway.ZhipuFallbackFirstOutputTimeoutSeconds <= 0 {
		return 0
	}
	return time.Duration(s.cfg.Gateway.ZhipuFallbackFirstOutputTimeoutSeconds) * time.Second
}

// zhipuFallbackRequestedModel returns the client-visible model for ops
// attribution; the guarded path must not hardcode one Zhipu model name.
func zhipuFallbackRequestedModel(ctx context.Context) string {
	if model, ok := RequestedPublicModelFromContext(ctx); ok {
		if trimmed := strings.TrimSpace(model); trimmed != "" {
			return trimmed
		}
	}
	return "zhipu"
}
