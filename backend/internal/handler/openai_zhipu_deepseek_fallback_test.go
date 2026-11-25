//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 智谱 -> DeepSeek 本地模型路由（单 Key 灰度）的 handler 级测试。
//
// 夹具走 standard 运行模式：真实的 OpenAIGatewayService / APIKeyService /
// BillingService 调度与计费链路，仅把 HTTP 上游、账号/分组/用量/配额仓储与余额缓存
// 换成内存桩。断言全部是黑盒可观测量：上游调用序列、出站请求、客户端响应、落账归属。

const (
	zhipuDSFallbackSourceGroupID   = int64(9401)
	zhipuDSFallbackSourceKeyID     = int64(9402)
	zhipuDSFallbackUserID          = int64(9403)
	zhipuDSFallbackTargetGroupID   = int64(9404)
	zhipuDSFallbackSourceAccountID = int64(9405)
	zhipuDSFallbackTargetAccountID = int64(9406)

	zhipuDSFallbackSourceModel = "glm-5.3"
	zhipuDSFallbackTargetModel = "deepseek-v4-pro"

	// 上游成功应答里的 token 用量，供落账断言使用。
	zhipuDSFallbackInputTokens  = 5
	zhipuDSFallbackOutputTokens = 2

	zhipuDSFallbackSourceAnswer    = "answer-from-glm"
	zhipuDSFallbackTargetAnswer    = "answer-from-deepseek"
	zhipuDSFallbackPartialAnswer   = "partial-answer-from-glm"
	zhipuDSFallbackSourceErrorText = "glm-source-rate-limited"
)

// zhipuDSFallbackStreamFailure 是一条流内的瞬时失败事件（HTTP 200 之后才到达）。
type zhipuDSFallbackStreamFailure struct {
	name  string
	event string
}

// zhipuDSFallbackEndpoint 描述一个入口及其供应商原生协议（来源与目标账号共用）。
type zhipuDSFallbackEndpoint struct {
	name string
	path string
	// protocol 即 credentials.api_protocol。
	protocol string
	// upstreamPath 是该协议下上游端点的路径后缀。
	upstreamPath string
	// request 构建请求体。
	request func(stream bool) string
	call    func(*OpenAIGatewayHandler, *gin.Context)
	// success / streamSuccess 构建该协议下非流式 / 流式的成功应答。
	success       func(model, answer string) zhipuDSFallbackReply
	streamSuccess func(model, answer string) zhipuDSFallbackReply
	// answerOf 从非流式客户端响应中取出助手回答。
	answerOf func(body string) string
	// streamFailures 是该协议下的流内瞬时失败事件。
	streamFailures []zhipuDSFallbackStreamFailure
	// streamMetadata 是尚未向客户端提交任何字节的生命周期帧（含来源响应 ID）。
	streamMetadata string
	// streamIDOnly / streamOutput 是「仅响应 ID 已下发」/「语义输出已下发」的来源流前缀。
	streamIDOnly   string
	streamOutput   string
	sourceStreamID string
}

const (
	zhipuDSFallbackSourcePartialChunk = `"id":"chatcmpl_src","object":"chat.completion.chunk","model":"` + zhipuDSFallbackSourceModel + `"`
)

var zhipuDSFallbackEndpoints = []zhipuDSFallbackEndpoint{
	{
		name:         "responses",
		path:         "/v1/responses",
		protocol:     service.APIProtocolResponses,
		upstreamPath: "/responses",
		request: func(stream bool) string {
			return `{"model":"` + zhipuDSFallbackSourceModel + `","input":"hello","stream":` + zhipuDSFallbackBool(stream) + `}`
		},
		call: (*OpenAIGatewayHandler).Responses,
		success: func(model, answer string) zhipuDSFallbackReply {
			return zhipuDSFallbackJSONReply(http.StatusOK, `{"id":"resp_upstream","object":"response","status":"completed","model":"`+model+`",`+
				`"output":[{"type":"message","id":"msg_upstream","role":"assistant","status":"completed",`+
				`"content":[{"type":"output_text","text":"`+answer+`","annotations":[]}]}],`+
				`"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`)
		},
		streamSuccess: func(model, answer string) zhipuDSFallbackReply {
			return zhipuDSFallbackSSEReply(
				zhipuDSFallbackResponseCreated("resp_upstream", model) +
					zhipuDSFallbackResponseItemAdded("msg_upstream") +
					"event: response.output_text.delta\n" +
					`data: {"type":"response.output_text.delta","item_id":"msg_upstream","output_index":0,"content_index":0,"delta":"` + answer + `"}` + "\n\n" +
					"event: response.completed\n" +
					`data: {"type":"response.completed","response":{"id":"resp_upstream","object":"response","status":"completed","model":"` + model + `",` +
					`"output":[{"type":"message","id":"msg_upstream","role":"assistant","status":"completed","content":[{"type":"output_text","text":"` + answer + `","annotations":[]}]}],` +
					`"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}` + "\n\n")
		},
		answerOf: func(body string) string {
			return gjson.Get(body, "output.0.content.0.text").String()
		},
		streamFailures: []zhipuDSFallbackStreamFailure{
			{name: "response.failed", event: "event: response.failed\n" +
				`data: {"type":"response.failed","response":{"id":"resp_src","object":"response","status":"failed","model":"` + zhipuDSFallbackSourceModel + `","output":[],` +
				`"error":{"code":"rate_limit_exceeded","message":"` + zhipuDSFallbackSourceErrorText + `"}}}` + "\n\n"},
		},
		streamMetadata: zhipuDSFallbackResponseCreated("resp_src", zhipuDSFallbackSourceModel),
		streamIDOnly:   zhipuDSFallbackResponseCreated("resp_src", zhipuDSFallbackSourceModel) + zhipuDSFallbackResponseReasoningAdded("rs_src"),
		streamOutput:   zhipuDSFallbackResponseCreated("resp_src", zhipuDSFallbackSourceModel) + zhipuDSFallbackResponseItemAdded("msg_src") + zhipuDSFallbackResponseTextDelta("msg_src", zhipuDSFallbackPartialAnswer),
		sourceStreamID: "resp_src",
	},
	{
		name:         "chat_completions",
		path:         "/v1/chat/completions",
		protocol:     service.APIProtocolChatCompletions,
		upstreamPath: "/chat/completions",
		request: func(stream bool) string {
			return `{"model":"` + zhipuDSFallbackSourceModel + `","messages":[{"role":"user","content":"hello"}],"stream":` + zhipuDSFallbackBool(stream) + `}`
		},
		call: (*OpenAIGatewayHandler).ChatCompletions,
		success: func(model, answer string) zhipuDSFallbackReply {
			return zhipuDSFallbackJSONReply(http.StatusOK, `{"id":"chatcmpl_upstream","object":"chat.completion","model":"`+model+`",`+
				`"choices":[{"index":0,"message":{"role":"assistant","content":"`+answer+`"},"finish_reason":"stop"}],`+
				`"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`)
		},
		streamSuccess: func(model, answer string) zhipuDSFallbackReply {
			chunk := `"id":"chatcmpl_upstream","object":"chat.completion.chunk","model":"` + model + `"`
			return zhipuDSFallbackSSEReply(
				`data: {` + chunk + `,"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}` + "\n\n" +
					`data: {` + chunk + `,"choices":[{"index":0,"delta":{"content":"` + answer + `"},"finish_reason":null}]}` + "\n\n" +
					`data: {` + chunk + `,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
					`data: {` + chunk + `,"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}` + "\n\n" +
					"data: [DONE]\n\n")
		},
		answerOf: func(body string) string {
			return gjson.Get(body, "choices.0.message.content").String()
		},
		streamFailures: []zhipuDSFallbackStreamFailure{
			{name: "error frame", event: `data: {"error":{"message":"` + zhipuDSFallbackSourceErrorText + `","type":"rate_limit_error"}}` + "\n\n"},
		},
		streamIDOnly: `data: {` + zhipuDSFallbackSourcePartialChunk + `,"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}` + "\n\n",
		streamOutput: `data: {` + zhipuDSFallbackSourcePartialChunk + `,"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}` + "\n\n" +
			`data: {` + zhipuDSFallbackSourcePartialChunk + `,"choices":[{"index":0,"delta":{"content":"` + zhipuDSFallbackPartialAnswer + `"},"finish_reason":null}]}` + "\n\n",
		sourceStreamID: "chatcmpl_src",
	},
	{
		name:         "messages",
		path:         "/v1/messages",
		protocol:     service.APIProtocolAnthropic,
		upstreamPath: "/v1/messages",
		request: func(stream bool) string {
			return `{"model":"` + zhipuDSFallbackSourceModel + `","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":` + zhipuDSFallbackBool(stream) + `}`
		},
		call: (*OpenAIGatewayHandler).Messages,
		success: func(model, answer string) zhipuDSFallbackReply {
			return zhipuDSFallbackJSONReply(http.StatusOK, `{"id":"msg_upstream","type":"message","role":"assistant","model":"`+model+`",`+
				`"content":[{"type":"text","text":"`+answer+`"}],"stop_reason":"end_turn",`+
				`"usage":{"input_tokens":5,"output_tokens":2}}`)
		},
		streamSuccess: func(model, answer string) zhipuDSFallbackReply {
			return zhipuDSFallbackSSEReply(
				"event: message_start\n" +
					`data: {"type":"message_start","message":{"id":"msg_upstream","type":"message","role":"assistant","model":"` + model + `","content":[],"stop_reason":null,"usage":{"input_tokens":5,"output_tokens":0}}}` + "\n\n" +
					"event: content_block_start\n" +
					`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
					"event: content_block_delta\n" +
					`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + answer + `"}}` + "\n\n" +
					"event: content_block_stop\n" +
					`data: {"type":"content_block_stop","index":0}` + "\n\n" +
					"event: message_delta\n" +
					`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}` + "\n\n" +
					"event: message_stop\n" +
					`data: {"type":"message_stop"}` + "\n\n")
		},
		answerOf: func(body string) string {
			return gjson.Get(body, "content.0.text").String()
		},
		streamFailures: []zhipuDSFallbackStreamFailure{
			{name: "rate_limit_error", event: zhipuDSFallbackAnthropicError("rate_limit_error", zhipuDSFallbackSourceErrorText)},
			{name: "overloaded_error", event: zhipuDSFallbackAnthropicError("overloaded_error", zhipuDSFallbackSourceErrorText)},
		},
		streamIDOnly:   zhipuDSFallbackMessageStart("msg_src"),
		streamOutput:   zhipuDSFallbackMessageStart("msg_src") + zhipuDSFallbackMessageTextDelta(zhipuDSFallbackPartialAnswer),
		sourceStreamID: "msg_src",
	},
}

func zhipuDSFallbackBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func zhipuDSFallbackResponseCreated(id, model string) string {
	return "event: response.created\n" +
		`data: {"type":"response.created","response":{"id":"` + id + `","object":"response","status":"in_progress","model":"` + model + `","output":[]}}` + "\n\n"
}

func zhipuDSFallbackResponseItemAdded(itemID string) string {
	return "event: response.output_item.added\n" +
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"` + itemID + `","role":"assistant","status":"in_progress","content":[]}}` + "\n\n"
}

func zhipuDSFallbackResponseReasoningAdded(itemID string) string {
	return "event: response.output_item.added\n" +
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"` + itemID + `","summary":[],"encrypted_content":"opaque-reasoning"}}` + "\n\n"
}

func zhipuDSFallbackResponseTextDelta(itemID, text string) string {
	return "event: response.output_text.delta\n" +
		`data: {"type":"response.output_text.delta","item_id":"` + itemID + `","output_index":0,"content_index":0,"delta":"` + text + `"}` + "\n\n"
}

func zhipuDSFallbackMessageStart(id string) string {
	return "event: message_start\n" +
		`data: {"type":"message_start","message":{"id":"` + id + `","type":"message","role":"assistant","model":"` + zhipuDSFallbackSourceModel + `","content":[],"stop_reason":null,"usage":{"input_tokens":5,"output_tokens":0}}}` + "\n\n"
}

func zhipuDSFallbackMessageTextDelta(text string) string {
	return "event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + text + `"}}` + "\n\n"
}

func zhipuDSFallbackAnthropicError(errType, message string) string {
	return "event: error\n" + `data: {"type":"error","error":{"type":"` + errType + `","message":"` + message + `"}}` + "\n\n"
}

// sourceCredentials 是智谱账号的凭证。GLM 仅 Coding Plan 提供原生 Responses 端点。
func (e zhipuDSFallbackEndpoint) sourceCredentials() map[string]any {
	credentials := map[string]any{"api_key": "zhipu-source-key", "api_protocol": e.protocol}
	if e.protocol == service.APIProtocolResponses {
		credentials["account_mode"] = service.AccountModeCoding
	}
	return credentials
}

func (e zhipuDSFallbackEndpoint) targetCredentials() map[string]any {
	return map[string]any{"api_key": "deepseek-target-key", "api_protocol": e.protocol}
}

func (e zhipuDSFallbackEndpoint) sourceSuccess() zhipuDSFallbackReply {
	return e.success(zhipuDSFallbackSourceModel, zhipuDSFallbackSourceAnswer)
}

func (e zhipuDSFallbackEndpoint) targetSuccess() zhipuDSFallbackReply {
	return e.success(zhipuDSFallbackTargetModel, zhipuDSFallbackTargetAnswer)
}

func (e zhipuDSFallbackEndpoint) targetStreamSuccess() zhipuDSFallbackReply {
	return e.streamSuccess(zhipuDSFallbackTargetModel, zhipuDSFallbackTargetAnswer)
}

// ---- HTTP 上游桩 ----

type zhipuDSFallbackReply struct {
	status      int
	contentType string
	body        string
}

func zhipuDSFallbackJSONReply(status int, body string) zhipuDSFallbackReply {
	return zhipuDSFallbackReply{status: status, contentType: "application/json", body: body}
}

func zhipuDSFallbackSSEReply(body string) zhipuDSFallbackReply {
	return zhipuDSFallbackReply{status: http.StatusOK, contentType: "text/event-stream", body: body}
}

// zhipuDSFallbackUpstreamError 是一条普通上游错误应答。文案刻意不含 model not found
// 字样：这类 400 会被网关归为「换号可解」并触发普通 failover，不代表客户端错误。
func zhipuDSFallbackUpstreamError(status int) zhipuDSFallbackReply {
	return zhipuDSFallbackJSONReply(status, `{"error":{"type":"upstream_error","message":"upstream rejected the request"}}`)
}

type zhipuDSFallbackUpstreamRequest struct {
	accountID int64
	url       string
	body      []byte
}

// zhipuDSFallbackUpstream 按账号编排应答：每个账号的应答按顺序消费，用尽后重复最后
// 一条；并记录每次上游调用的账号、URL 与请求体。
type zhipuDSFallbackUpstream struct {
	service.HTTPUpstream
	mu       sync.Mutex
	replies  map[int64][]zhipuDSFallbackReply
	requests []zhipuDSFallbackUpstreamRequest
}

func (u *zhipuDSFallbackUpstream) script(accountID int64, replies ...zhipuDSFallbackReply) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.replies == nil {
		u.replies = map[int64][]zhipuDSFallbackReply{}
	}
	u.replies[accountID] = replies
}

func (u *zhipuDSFallbackUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.requests = append(u.requests, zhipuDSFallbackUpstreamRequest{
		accountID: accountID,
		url:       req.URL.String(),
		body:      append([]byte(nil), body...),
	})
	queued := u.replies[accountID]
	reply := zhipuDSFallbackJSONReply(http.StatusInternalServerError, `{"error":{"message":"no scripted upstream reply"}}`)
	if len(queued) > 0 {
		reply = queued[0]
		if len(queued) > 1 {
			u.replies[accountID] = queued[1:]
		}
	}
	return &http.Response{
		StatusCode: reply.status,
		Header:     http.Header{"Content-Type": []string{reply.contentType}},
		Body:       io.NopCloser(bytes.NewBufferString(reply.body)),
	}, nil
}

func (u *zhipuDSFallbackUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func (u *zhipuDSFallbackUpstream) calls() []zhipuDSFallbackUpstreamRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]zhipuDSFallbackUpstreamRequest(nil), u.requests...)
}

func (u *zhipuDSFallbackUpstream) accountIDs() []int64 {
	calls := u.calls()
	ids := make([]int64, 0, len(calls))
	for _, call := range calls {
		ids = append(ids, call.accountID)
	}
	return ids
}

// ---- 调度器夹具：按分组 + 平台列出来源/目标账号 ----

type zhipuDSFallbackSelectionQuery struct {
	groupID  int64
	platform string
}

type zhipuDSFallbackAccountRepo struct {
	service.AccountRepository
	mu       sync.Mutex
	accounts []service.Account
	queries  []zhipuDSFallbackSelectionQuery
}

func (r *zhipuDSFallbackAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			account.Credentials = cloneCredentialMap(account.Credentials)
			return &account, nil
		}
	}
	return nil, nil
}

func (r *zhipuDSFallbackAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, platform string) ([]service.Account, error) {
	r.mu.Lock()
	r.queries = append(r.queries, zhipuDSFallbackSelectionQuery{groupID: groupID, platform: platform})
	r.mu.Unlock()
	return r.list(groupID, platform), nil
}

func (r *zhipuDSFallbackAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.list(0, platform), nil
}

func (r *zhipuDSFallbackAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.list(0, platform), nil
}

func (r *zhipuDSFallbackAccountRepo) list(groupID int64, platform string) []service.Account {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform != platform || !account.IsSchedulable() {
			continue
		}
		if groupID != 0 && !zhipuDSFallbackAccountInGroup(account, groupID) {
			continue
		}
		out = append(out, account)
	}
	return out
}

func (r *zhipuDSFallbackAccountRepo) selectionQueries() []zhipuDSFallbackSelectionQuery {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]zhipuDSFallbackSelectionQuery(nil), r.queries...)
}

func zhipuDSFallbackAccountInGroup(account service.Account, groupID int64) bool {
	for _, id := range account.GroupIDs {
		if id == groupID {
			return true
		}
	}
	for _, accountGroup := range account.AccountGroups {
		if accountGroup.GroupID == groupID {
			return true
		}
	}
	return false
}

// ---- 落账 / 配额观测点 ----

// zhipuDSFallbackUsageLogRepo 捕获落库的用量行。
type zhipuDSFallbackUsageLogRepo struct {
	service.UsageLogRepository
	mu   sync.Mutex
	logs []service.UsageLog
}

func (r *zhipuDSFallbackUsageLogRepo) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, *log)
	return true, nil
}

func (r *zhipuDSFallbackUsageLogRepo) recorded() []service.UsageLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]service.UsageLog(nil), r.logs...)
}

// zhipuDSFallbackUsageBillingRepo 捕获统一扣费命令（生产路径的原子扣费入口）。
type zhipuDSFallbackUsageBillingRepo struct {
	service.UsageBillingRepository
	mu       sync.Mutex
	commands []service.UsageBillingCommand
}

func (r *zhipuDSFallbackUsageBillingRepo) Apply(_ context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, *cmd)
	return &service.UsageBillingApplyResult{Applied: true}, nil
}

func (r *zhipuDSFallbackUsageBillingRepo) recorded() []service.UsageBillingCommand {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]service.UsageBillingCommand(nil), r.commands...)
}

// zhipuDSFallbackQuotaRepo 是 user x platform 配额仓储桩：只对 deepseek 平台返回记录。
type zhipuDSFallbackQuotaRepo struct {
	service.UserPlatformQuotaRepository
	record *service.UserPlatformQuotaRecord
}

func (r *zhipuDSFallbackQuotaRepo) GetByUserPlatform(_ context.Context, _ int64, platform string) (*service.UserPlatformQuotaRecord, error) {
	if platform != service.PlatformDeepseek || r.record == nil {
		return nil, nil
	}
	record := *r.record
	return &record, nil
}

func (r *zhipuDSFallbackQuotaRepo) IncrementUsageWithReset(context.Context, int64, string, float64, time.Time) error {
	return nil
}

// zhipuDSFallbackRPMCache 是 (user, group) RPM 计数桩：按分组返回配置好的最新计数。
type zhipuDSFallbackRPMCache struct {
	service.UserRPMCache
	mu          sync.Mutex
	groupCounts map[int64]int
	counted     []int64
}

func (c *zhipuDSFallbackRPMCache) IncrementUserGroupRPM(_ context.Context, _ int64, groupID int64) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counted = append(c.counted, groupID)
	if count, ok := c.groupCounts[groupID]; ok {
		return count, nil
	}
	return 1, nil
}

func (c *zhipuDSFallbackRPMCache) countedGroups() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int64(nil), c.counted...)
}

// zhipuDSFallbackBillingCache 是内存余额缓存 + 在途预留 + 平台配额缓存，并记录每次
// 预留所属的用户与每次扣减。
type zhipuDSFallbackBillingCache struct {
	service.BillingCache
	mu           sync.Mutex
	balance      float64
	reservations map[string]zhipuDSFallbackReservation
	quota        map[string]*service.UserPlatformQuotaCacheEntry
	quotaUsed    map[string]float64
}

type zhipuDSFallbackReservation struct {
	userID int64
	amount float64
}

func newZhipuDSFallbackBillingCache(balance float64) *zhipuDSFallbackBillingCache {
	return &zhipuDSFallbackBillingCache{
		balance:      balance,
		reservations: map[string]zhipuDSFallbackReservation{},
		quota:        map[string]*service.UserPlatformQuotaCacheEntry{},
		quotaUsed:    map[string]float64{},
	}
}

func (m *zhipuDSFallbackBillingCache) GetUserBalance(context.Context, int64) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.balance, nil
}

func (m *zhipuDSFallbackBillingCache) DeductUserBalance(_ context.Context, userID int64, amount float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.balance -= amount
	return nil
}

func (m *zhipuDSFallbackBillingCache) InvalidateUserBalance(context.Context, int64) error {
	return nil
}

func (m *zhipuDSFallbackBillingCache) ReserveInflightBalance(_ context.Context, userID int64, id string, amount, _ float64, _ time.Duration) (bool, float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	held := 0.0
	for _, reservation := range m.reservations {
		held += reservation.amount
	}
	if m.balance-held < amount {
		return false, held, nil
	}
	m.reservations[id] = zhipuDSFallbackReservation{userID: userID, amount: amount}
	return true, held, nil
}

func (m *zhipuDSFallbackBillingCache) ReleaseInflightBalance(_ context.Context, _ int64, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.reservations, id)
	return nil
}

func (m *zhipuDSFallbackBillingCache) GetUserPlatformQuotaCache(_ context.Context, _ int64, platform string) (*service.UserPlatformQuotaCacheEntry, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.quota[platform]
	if !ok {
		return nil, false, nil
	}
	copied := *entry
	return &copied, true, nil
}

func (m *zhipuDSFallbackBillingCache) SetUserPlatformQuotaCache(_ context.Context, _ int64, platform string, entry *service.UserPlatformQuotaCacheEntry, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := *entry
	m.quota[platform] = &copied
	return nil
}

func (m *zhipuDSFallbackBillingCache) IncrUserPlatformQuotaUsageCache(_ context.Context, _ int64, platform string, cost float64, _ time.Duration, _ bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, cached := m.quota[platform]; cached {
		m.quotaUsed[platform] += cost
	}
	return nil
}

// snapshot 返回在途预留所属用户、各平台配额累计用量与余额缓存。
func (m *zhipuDSFallbackBillingCache) snapshot() (reservedFor []int64, quotaUsed map[string]float64, balance float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, reservation := range m.reservations {
		reservedFor = append(reservedFor, reservation.userID)
	}
	quotaUsed = map[string]float64{}
	for platform, used := range m.quotaUsed {
		quotaUsed[platform] = used
	}
	return reservedFor, quotaUsed, m.balance
}

// ---- 夹具 ----

type zhipuDSFallbackFixture struct {
	handler        *OpenAIGatewayHandler
	cfg            *config.Config
	endpoint       zhipuDSFallbackEndpoint
	upstream       *zhipuDSFallbackUpstream
	accountRepo    *zhipuDSFallbackAccountRepo
	groups         map[int64]*service.Group
	user           *service.User
	apiKey         *service.APIKey
	usageLogs      *zhipuDSFallbackUsageLogRepo
	billing        *zhipuDSFallbackUsageBillingRepo
	quotaRepo      *zhipuDSFallbackQuotaRepo
	rpmCache       *zhipuDSFallbackRPMCache
	cache          *zhipuDSFallbackBillingCache
	billingService *service.BillingService
	pricing        *service.ModelPricingResolver
}

func zhipuDSFallbackAccount(id, groupID int64, platform string, credentials map[string]any) service.Account {
	return service.Account{
		ID:          id,
		Name:        platform + "-account",
		Platform:    platform,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: credentials,
		GroupIDs:    []int64{groupID},
	}
}

func newZhipuDSFallbackFixture(t *testing.T, endpoint zhipuDSFallbackEndpoint) *zhipuDSFallbackFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	sourceGroup := &service.Group{
		ID: zhipuDSFallbackSourceGroupID, Name: "zhipu-glm", Platform: service.PlatformZhipu,
		Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1,
	}
	// 目标分组是专属分组：只有被授权（AllowedGroups）的用户才可绑定。
	targetGroup := &service.Group{
		ID: zhipuDSFallbackTargetGroupID, Name: "deepseek-local", Platform: service.PlatformDeepseek,
		Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1,
		IsExclusive: true,
	}
	groups := map[int64]*service.Group{
		zhipuDSFallbackSourceGroupID: sourceGroup,
		zhipuDSFallbackTargetGroupID: targetGroup,
	}
	user := &service.User{
		ID: zhipuDSFallbackUserID, Status: service.StatusActive, Balance: 100, Concurrency: 2,
		AllowedGroups: []int64{zhipuDSFallbackTargetGroupID},
	}
	sourceKey := &service.APIKey{
		ID: zhipuDSFallbackSourceKeyID, UserID: zhipuDSFallbackUserID,
		GroupID: &sourceGroup.ID, Group: sourceGroup, Status: service.StatusActive, User: user,
	}

	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
	cfg.Gateway.ZhipuDeepSeekFallback = config.GatewayZhipuDeepSeekFallbackConfig{
		Enabled:       true,
		SourceGroupID: zhipuDSFallbackSourceGroupID,
		SourceKeyID:   zhipuDSFallbackSourceKeyID,
		TargetGroupID: zhipuDSFallbackTargetGroupID,
		TargetModel:   zhipuDSFallbackTargetModel,
	}

	accountRepo := &zhipuDSFallbackAccountRepo{accounts: []service.Account{
		zhipuDSFallbackAccount(zhipuDSFallbackSourceAccountID, zhipuDSFallbackSourceGroupID, service.PlatformZhipu, endpoint.sourceCredentials()),
		zhipuDSFallbackAccount(zhipuDSFallbackTargetAccountID, zhipuDSFallbackTargetGroupID, service.PlatformDeepseek, endpoint.targetCredentials()),
	}}
	usageLogs := &zhipuDSFallbackUsageLogRepo{}
	billing := &zhipuDSFallbackUsageBillingRepo{}
	quotaRepo := &zhipuDSFallbackQuotaRepo{}
	rpmCache := &zhipuDSFallbackRPMCache{}
	cache := newZhipuDSFallbackBillingCache(user.Balance)
	billingCache := service.NewBillingCacheService(cache, nil, nil, nil, rpmCache, nil, cfg, quotaRepo)
	t.Cleanup(billingCache.Stop)
	billingService := service.NewBillingService(cfg, nil)
	pricing := service.NewModelPricingResolver(nil, billingService)
	concurrency := service.NewConcurrencyService(&fakeConcurrencyCache{})
	upstream := &zhipuDSFallbackUpstream{}

	gateway := service.NewOpenAIGatewayService(
		accountRepo,
		usageLogs,
		billing,
		nil, // userRepo
		nil, // userSubRepo
		nil, // userGroupRateRepo
		nil, // cache（关闭粘性会话）
		cfg,
		nil, // schedulerSnapshot：走账号仓储的 legacy 调度路径
		concurrency,
		billingService,
		nil, // rateLimitService
		billingCache,
		upstream,
		&service.DeferredService{},
		nil, // openAITokenProvider
		nil, // grokTokenProvider
		pricing,
		nil, // channelService
		nil, // balanceNotifyService
		nil, // settingService
		quotaRepo,
	)
	apiKeyService := service.NewAPIKeyService(nil, nil, &groupMapRepo{fakeGroupRepo: &fakeGroupRepo{}, groups: groups}, nil, nil, nil, cfg)
	handler := NewOpenAIGatewayHandler(gateway, concurrency, billingCache, apiKeyService, nil, nil, nil, nil, cfg)

	return &zhipuDSFallbackFixture{
		handler:        handler,
		cfg:            cfg,
		endpoint:       endpoint,
		upstream:       upstream,
		accountRepo:    accountRepo,
		groups:         groups,
		user:           user,
		apiKey:         sourceKey,
		usageLogs:      usageLogs,
		billing:        billing,
		quotaRepo:      quotaRepo,
		rpmCache:       rpmCache,
		cache:          cache,
		billingService: billingService,
		pricing:        pricing,
	}
}

// addSourceAccount 往来源分组追加一个智谱账号（与首个账号同优先级，排在它之后）。
func (f *zhipuDSFallbackFixture) addSourceAccount(accountID int64) {
	f.accountRepo.mu.Lock()
	defer f.accountRepo.mu.Unlock()
	f.accountRepo.accounts = append(f.accountRepo.accounts, zhipuDSFallbackAccount(
		accountID, zhipuDSFallbackSourceGroupID, service.PlatformZhipu, f.endpoint.sourceCredentials(),
	))
}

func (f *zhipuDSFallbackFixture) serve(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, f.endpoint.path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), f.apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: f.user.ID, Concurrency: f.user.Concurrency})
	f.endpoint.call(f.handler, c)
	return recorder
}

// ---- 断言 ----

// requireAttempt 断言一次上游尝试命中的账号、供应商原生协议端点与模型。
func (f *zhipuDSFallbackFixture) requireAttempt(t *testing.T, request zhipuDSFallbackUpstreamRequest, accountID int64, model string) {
	t.Helper()
	require.Equal(t, accountID, request.accountID, "upstream attempt must hit the expected account")
	parsed, err := url.Parse(request.url)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(parsed.Path, f.endpoint.upstreamPath),
		"upstream attempt must use the %s native endpoint, got %s", f.endpoint.protocol, request.url)
	require.Equal(t, model, gjson.GetBytes(request.body, "model").String(), "upstream attempt must carry the expected model")
}

// requireAttempts 按顺序断言本次请求的全部上游尝试。
type zhipuDSFallbackAttempt struct {
	accountID int64
	model     string
}

func (f *zhipuDSFallbackFixture) requireAttempts(t *testing.T, want []zhipuDSFallbackAttempt) {
	t.Helper()
	calls := f.upstream.calls()
	require.Len(t, calls, len(want), "unexpected upstream attempt count: got account IDs %v", f.upstream.accountIDs())
	for i, attempt := range want {
		f.requireAttempt(t, calls[i], attempt.accountID, attempt.model)
	}
	for _, call := range calls {
		require.Equal(t, gjson.GetBytes(calls[0].body, "stream").Bool(), gjson.GetBytes(call.body, "stream").Bool(),
			"an attempt must preserve the client's stream flag")
	}
}

// requireScheduledFromTargetGroup 断言选号查询依次全部落在来源分组，最后一次才查询
// 目标分组（平台 deepseek）——回退尝试是经目标分组调度出来的。
func (f *zhipuDSFallbackFixture) requireScheduledFromTargetGroup(t *testing.T) {
	t.Helper()
	queries := f.accountRepo.selectionQueries()
	require.NotEmpty(t, queries)
	source := zhipuDSFallbackSelectionQuery{groupID: zhipuDSFallbackSourceGroupID, platform: service.PlatformZhipu}
	target := zhipuDSFallbackSelectionQuery{groupID: zhipuDSFallbackTargetGroupID, platform: service.PlatformDeepseek}
	require.Equal(t, target, queries[len(queries)-1], "the fallback attempt must be scheduled from the target group")
	for _, query := range queries[:len(queries)-1] {
		require.Equal(t, source, query, "primary attempts must be scheduled from the source group")
	}
}

// requireBilledToSourceKey 断言回退成功的这一次用量落在原个人余额 Key 上：同一 Key、
// 同一用户、余额计费、由目标账号按目标模型服务，且在途预留全部归还。
func (f *zhipuDSFallbackFixture) requireBilledToSourceKey(t *testing.T) {
	t.Helper()

	logs := f.usageLogs.recorded()
	require.Len(t, logs, 1, "exactly the successful fallback attempt is billable")
	log := logs[0]
	require.Equal(t, zhipuDSFallbackSourceKeyID, log.APIKeyID)
	require.Equal(t, zhipuDSFallbackUserID, log.UserID)
	require.Equal(t, zhipuDSFallbackTargetAccountID, log.AccountID)
	require.Equal(t, zhipuDSFallbackTargetModel, log.Model)
	require.Equal(t, zhipuDSFallbackSourceModel, log.RequestedModel)
	require.Nil(t, log.SubscriptionID, "fallback must not be billed as a subscription")
	require.Equal(t, service.BillingTypeBalance, log.BillingType)
	require.Equal(t, zhipuDSFallbackInputTokens, log.InputTokens)
	require.Equal(t, zhipuDSFallbackOutputTokens, log.OutputTokens)
	require.Greater(t, log.ActualCost, 0.0, "fallback must be billed at the target model price")

	commands := f.billing.recorded()
	require.Len(t, commands, 1)
	require.Equal(t, zhipuDSFallbackSourceKeyID, commands[0].APIKeyID)
	require.Equal(t, zhipuDSFallbackUserID, commands[0].UserID)
	require.Equal(t, zhipuDSFallbackTargetAccountID, commands[0].AccountID)
	require.Equal(t, zhipuDSFallbackTargetModel, commands[0].Model)
	require.Nil(t, commands[0].SubscriptionID)
	require.InDelta(t, log.ActualCost, commands[0].BalanceCost, 1e-8)

	f.requireNoLeakedReservation(t)
}

// requireNoLeakedReservation 断言请求结束后没有残留的在途预留。
func (f *zhipuDSFallbackFixture) requireNoLeakedReservation(t *testing.T) {
	t.Helper()
	reservedFor, _, _ := f.cache.snapshot()
	require.Empty(t, reservedFor, "no inflight reservation may leak after the request")
}

// requireNotBilledForTarget 断言目标账号没有产生任何落账或扣费。
func (f *zhipuDSFallbackFixture) requireNotBilledForTarget(t *testing.T) {
	t.Helper()
	for _, log := range f.usageLogs.recorded() {
		require.NotEqual(t, zhipuDSFallbackTargetAccountID, log.AccountID, "target account must not be billed")
	}
	for _, command := range f.billing.recorded() {
		require.NotEqual(t, zhipuDSFallbackTargetAccountID, command.AccountID, "target account must not be charged")
	}
	f.requireNoLeakedReservation(t)
}

var zhipuDSFallbackPrimaryAttempt = zhipuDSFallbackAttempt{accountID: zhipuDSFallbackSourceAccountID, model: zhipuDSFallbackSourceModel}
var zhipuDSFallbackTargetAttempt = zhipuDSFallbackAttempt{accountID: zhipuDSFallbackTargetAccountID, model: zhipuDSFallbackTargetModel}

// requireNoFallback 断言上游只尝试了给定的来源账号（各一次、按顺序），调度器从未被
// 问及目标分组，客户端也没有拿到目标模型的回答，且没有任何字节落到目标账号的账上。
func (f *zhipuDSFallbackFixture) requireNoFallback(t *testing.T, recorder *httptest.ResponseRecorder, primary ...zhipuDSFallbackAttempt) {
	t.Helper()
	f.requireAttempts(t, primary)
	for _, query := range f.accountRepo.selectionQueries() {
		require.NotEqual(t, zhipuDSFallbackTargetGroupID, query.groupID, "the scheduler must never be asked for the target group")
	}
	require.NotContains(t, recorder.Body.String(), zhipuDSFallbackTargetAnswer)
	f.requireNotBilledForTarget(t)
}

// requireFallsBack 断言请求完成了一次回退：来源账号各尝试一次后目标账号恰好一次，
// 客户端拿到目标回答，落账在原 Key 上。响应体中不得残留来源账号的任何字节。
func (f *zhipuDSFallbackFixture) requireFallsBack(t *testing.T, recorder *httptest.ResponseRecorder, primary ...zhipuDSFallbackAttempt) {
	t.Helper()
	if len(primary) == 0 {
		primary = []zhipuDSFallbackAttempt{zhipuDSFallbackPrimaryAttempt}
	}
	f.requireAttempts(t, append(append([]zhipuDSFallbackAttempt(nil), primary...), zhipuDSFallbackTargetAttempt))
	f.requireScheduledFromTargetGroup(t)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	body := recorder.Body.String()
	require.Contains(t, body, zhipuDSFallbackTargetAnswer)
	require.NotContains(t, body, zhipuDSFallbackSourceAnswer)
	require.NotContains(t, body, zhipuDSFallbackPartialAnswer)
	require.NotContains(t, body, zhipuDSFallbackSourceErrorText)
	f.requireBilledToSourceKey(t)
}

// quote 返回真实计费引擎对「模型 + 上游应答里的 token 用量 + 分组倍率」在 at 时刻的报价。
func (f *zhipuDSFallbackFixture) quote(t *testing.T, model string, group *service.Group, at time.Time) *service.CostBreakdown {
	t.Helper()
	cost, err := f.billingService.CalculateCostUnified(service.CostInput{
		Ctx:            context.Background(),
		Model:          model,
		GroupID:        &group.ID,
		Group:          group,
		Tokens:         service.UsageTokens{InputTokens: zhipuDSFallbackInputTokens, OutputTokens: zhipuDSFallbackOutputTokens},
		RequestCount:   1,
		RateMultiplier: group.RateMultiplier,
		PricingAt:      at,
		Resolver:       f.pricing,
	})
	require.NoError(t, err)
	return cost
}

func (f *zhipuDSFallbackFixture) quotaRecord(dailyLimit, dailyUsage float64) *service.UserPlatformQuotaRecord {
	now := time.Now()
	return &service.UserPlatformQuotaRecord{
		UserID: zhipuDSFallbackUserID, Platform: service.PlatformDeepseek,
		DailyLimitUSD: &dailyLimit, DailyUsageUSD: dailyUsage,
		DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now,
	}
}

// zhipuDSFallbackCase 是一个「主请求 503、目标账号本可成功」的场景变体。
type zhipuDSFallbackCase struct {
	name   string
	mutate func(f *zhipuDSFallbackFixture)
	source func(e zhipuDSFallbackEndpoint) zhipuDSFallbackReply
}

// 来源账号 429 / 502 / 503：各入口按供应商原生协议先请求 glm-5.3，失败后恰好一次改用
// 目标分组的 deepseek-v4-pro，并把这一次用量记在原个人余额 Key 上。
func TestZhipuDeepSeekFallback_RetriesTargetModelOnTransientUpstreamFailure(t *testing.T) {
	for _, endpoint := range zhipuDSFallbackEndpoints {
		for _, status := range []int{http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable} {
			t.Run(endpoint.name+"/"+http.StatusText(status), func(t *testing.T) {
				f := newZhipuDSFallbackFixture(t, endpoint)
				f.upstream.script(zhipuDSFallbackSourceAccountID, zhipuDSFallbackUpstreamError(status))
				f.upstream.script(zhipuDSFallbackTargetAccountID, endpoint.targetSuccess())

				recorder := f.serve(t, endpoint.request(false))

				f.requireFallsBack(t, recorder)
				require.Equal(t, zhipuDSFallbackTargetAnswer, endpoint.answerOf(recorder.Body.String()))
			})
		}
	}
}

// 来源分组有多个智谱账号：必须先按普通换号把来源池用完，才做一次模型回退；目标也失败
// 时立即终止，不重试目标、不再回到来源池。
func TestZhipuDeepSeekFallback_ExhaustsPrimaryPoolThenFallsBackOnlyOnce(t *testing.T) {
	const secondSourceAccountID = int64(9407)
	for _, endpoint := range zhipuDSFallbackEndpoints {
		for _, tc := range []struct {
			name         string
			targetStatus int
			wantOK       bool
		}{
			{name: "both primary accounts fail then target succeeds", targetStatus: http.StatusOK, wantOK: true},
			{name: "target also fails", targetStatus: http.StatusServiceUnavailable},
		} {
			t.Run(endpoint.name+"/"+tc.name, func(t *testing.T) {
				f := newZhipuDSFallbackFixture(t, endpoint)
				f.addSourceAccount(secondSourceAccountID)
				f.upstream.script(zhipuDSFallbackSourceAccountID, zhipuDSFallbackUpstreamError(http.StatusServiceUnavailable))
				f.upstream.script(secondSourceAccountID, zhipuDSFallbackUpstreamError(http.StatusServiceUnavailable))
				if tc.targetStatus == http.StatusOK {
					f.upstream.script(zhipuDSFallbackTargetAccountID, endpoint.targetSuccess())
				} else {
					f.upstream.script(zhipuDSFallbackTargetAccountID, zhipuDSFallbackUpstreamError(tc.targetStatus))
				}
				primary := []zhipuDSFallbackAttempt{
					zhipuDSFallbackPrimaryAttempt,
					{accountID: secondSourceAccountID, model: zhipuDSFallbackSourceModel},
				}

				recorder := f.serve(t, endpoint.request(false))

				if tc.wantOK {
					f.requireFallsBack(t, recorder, primary...)
					return
				}
				f.requireAttempts(t, append(append([]zhipuDSFallbackAttempt(nil), primary...), zhipuDSFallbackTargetAttempt))
				f.requireScheduledFromTargetGroup(t)
				require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
				f.requireNothingBilled(t)
			})
		}
	}
}

// 回退这一次按目标模型的真实报价计费：token 单价 x 上游上报用量，再乘目标分组倍率；
// 不得沿用来源模型 glm-5.3 的价格，余额缓存按同一金额扣减。
func TestZhipuDeepSeekFallback_BillsActualTargetModelPriceAtTargetGroupRate(t *testing.T) {
	for _, endpoint := range zhipuDSFallbackEndpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			f := newZhipuDSFallbackFixture(t, endpoint)
			targetGroup := f.groups[zhipuDSFallbackTargetGroupID]
			targetGroup.RateMultiplier = 0.8
			f.upstream.script(zhipuDSFallbackSourceAccountID, zhipuDSFallbackUpstreamError(http.StatusServiceUnavailable))
			f.upstream.script(zhipuDSFallbackTargetAccountID, endpoint.targetSuccess())

			startedAt := time.Now()
			recorder := f.serve(t, endpoint.request(false))
			finishedAt := time.Now()

			f.requireFallsBack(t, recorder)

			// 请求跨越分时定价边界时无法在测试里复现同一报价：直接判失败，不放行。
			atStart := f.quote(t, zhipuDSFallbackTargetModel, targetGroup, startedAt)
			atEnd := f.quote(t, zhipuDSFallbackTargetModel, targetGroup, finishedAt)
			require.Equal(t, atStart.TotalCost, atEnd.TotalCost, "request crossed a pricing window boundary; rerun")

			log := f.usageLogs.recorded()[0]
			require.InDelta(t, atStart.TotalCost, log.TotalCost, 1e-12, "usage must be priced with the target model's token rates")
			require.InDelta(t, atStart.TotalCost*targetGroup.RateMultiplier, log.ActualCost, 1e-12, "usage must be charged at the target group's rate")
			require.InDelta(t, atStart.ActualCost, f.billing.recorded()[0].BalanceCost, 1e-8, "the balance charge must equal the billed usage")

			sourceModelQuote := f.quote(t, zhipuDSFallbackSourceModel, targetGroup, finishedAt)
			require.NotEqual(t, service.QuantizeUsageBillingAmount(sourceModelQuote.ActualCost), service.QuantizeUsageBillingAmount(log.ActualCost),
				"fallback must not be billed at the source model's price")

			_, _, balance := f.cache.snapshot()
			require.InDelta(t, 100-atStart.ActualCost, balance, 1e-8, "the personal balance cache must reflect exactly one charge")
		})
	}
}

// 目标账号同样失败：模型回退只做一次，不重试目标，也不产生任何落账。
func TestZhipuDeepSeekFallback_OnlyOnceWhenTargetAlsoFails(t *testing.T) {
	for _, endpoint := range zhipuDSFallbackEndpoints {
		for _, stream := range []bool{false, true} {
			t.Run(endpoint.name+"/stream="+zhipuDSFallbackBool(stream), func(t *testing.T) {
				f := newZhipuDSFallbackFixture(t, endpoint)
				f.upstream.script(zhipuDSFallbackSourceAccountID, zhipuDSFallbackUpstreamError(http.StatusServiceUnavailable))
				f.upstream.script(zhipuDSFallbackTargetAccountID, zhipuDSFallbackUpstreamError(http.StatusServiceUnavailable))

				recorder := f.serve(t, endpoint.request(stream))

				f.requireAttempts(t, []zhipuDSFallbackAttempt{zhipuDSFallbackPrimaryAttempt, zhipuDSFallbackTargetAttempt})
				require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
				require.NotContains(t, recorder.Body.String(), zhipuDSFallbackTargetAnswer)
				f.requireNothingBilled(t)
			})
		}
	}
}

// 这些情况下不做模型回退：上游 400（请求自身的问题）、功能关闭、主请求成功、
// 请求来自来源分组里的其他 Key（灰度只对配置的那一把 Key 生效）。
func TestZhipuDeepSeekFallback_NotApplied(t *testing.T) {
	for _, endpoint := range zhipuDSFallbackEndpoints {
		for _, tc := range []zhipuDSFallbackCase{
			{
				name: "upstream 400",
				source: func(zhipuDSFallbackEndpoint) zhipuDSFallbackReply {
					return zhipuDSFallbackUpstreamError(http.StatusBadRequest)
				},
			},
			{
				name:   "disabled",
				mutate: func(f *zhipuDSFallbackFixture) { f.cfg.Gateway.ZhipuDeepSeekFallback.Enabled = false },
				source: func(zhipuDSFallbackEndpoint) zhipuDSFallbackReply {
					return zhipuDSFallbackUpstreamError(http.StatusServiceUnavailable)
				},
			},
			{
				name:   "primary succeeds",
				source: func(e zhipuDSFallbackEndpoint) zhipuDSFallbackReply { return e.sourceSuccess() },
			},
			{
				name:   "another key in the source group",
				mutate: func(f *zhipuDSFallbackFixture) { f.apiKey.ID = zhipuDSFallbackSourceKeyID + 1 },
				source: func(zhipuDSFallbackEndpoint) zhipuDSFallbackReply {
					return zhipuDSFallbackUpstreamError(http.StatusServiceUnavailable)
				},
			},
		} {
			t.Run(endpoint.name+"/"+tc.name, func(t *testing.T) {
				f := newZhipuDSFallbackFixture(t, endpoint)
				if tc.mutate != nil {
					tc.mutate(f)
				}
				f.upstream.script(zhipuDSFallbackSourceAccountID, tc.source(endpoint))
				f.upstream.script(zhipuDSFallbackTargetAccountID, endpoint.targetSuccess())

				recorder := f.serve(t, endpoint.request(false))

				f.requireNoFallback(t, recorder, zhipuDSFallbackPrimaryAttempt)
				switch tc.name {
				case "primary succeeds":
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					require.Equal(t, zhipuDSFallbackSourceAnswer, endpoint.answerOf(recorder.Body.String()))
					logs := f.usageLogs.recorded()
					require.Len(t, logs, 1)
					require.Equal(t, zhipuDSFallbackSourceAccountID, logs[0].AccountID)
				case "upstream 400":
					require.Equal(t, http.StatusBadRequest, recorder.Code, "a client-caused 400 must surface as-is")
				default:
					require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
				}
			})
		}
	}
}

// 目标分组必须是「该用户被授权使用、活跃、具体（非 composite / 非订阅）、DeepSeek、
// 且有价」的分组，否则不做模型回退，客户端拿到的是主请求的失败。
func TestZhipuDeepSeekFallback_RequiresAuthorizedActiveConcretePricedDeepSeekTarget(t *testing.T) {
	for _, endpoint := range zhipuDSFallbackEndpoints {
		for _, tc := range []zhipuDSFallbackCase{
			{
				name:   "user is not allowed on the exclusive target",
				mutate: func(f *zhipuDSFallbackFixture) { f.user.AllowedGroups = nil },
			},
			{
				name: "public-group-restricted user is not allowed on the target",
				mutate: func(f *zhipuDSFallbackFixture) {
					f.groups[zhipuDSFallbackTargetGroupID].IsExclusive = false
					f.user.RestrictPublicGroups = true
					f.user.AllowedGroups = nil
				},
			},
			{
				name: "target model is outside the target group's model allowlist",
				mutate: func(f *zhipuDSFallbackFixture) {
					f.groups[zhipuDSFallbackTargetGroupID].ModelAllowlist = service.GroupModelAllowlist{
						Enabled: true, Models: []string{"deepseek-v4-flash"},
					}
				},
			},
			{
				name: "target group is inactive",
				mutate: func(f *zhipuDSFallbackFixture) {
					f.groups[zhipuDSFallbackTargetGroupID].Status = service.StatusDisabled
				},
			},
			{
				name: "target group is composite",
				mutate: func(f *zhipuDSFallbackFixture) {
					f.groups[zhipuDSFallbackTargetGroupID].Platform = service.PlatformComposite
				},
			},
			{
				name: "target group is subscription-based",
				mutate: func(f *zhipuDSFallbackFixture) {
					f.groups[zhipuDSFallbackTargetGroupID].SubscriptionType = service.SubscriptionTypeSubscription
				},
			},
			{
				name: "target group is not DeepSeek",
				mutate: func(f *zhipuDSFallbackFixture) {
					f.groups[zhipuDSFallbackTargetGroupID].Platform = service.PlatformZhipu
				},
			},
			{
				name:   "target group has no price",
				mutate: func(f *zhipuDSFallbackFixture) { f.groups[zhipuDSFallbackTargetGroupID].RateMultiplier = 0 },
			},
		} {
			t.Run(endpoint.name+"/"+tc.name, func(t *testing.T) {
				f := newZhipuDSFallbackFixture(t, endpoint)
				tc.mutate(f)
				f.upstream.script(zhipuDSFallbackSourceAccountID, zhipuDSFallbackUpstreamError(http.StatusServiceUnavailable))
				f.upstream.script(zhipuDSFallbackTargetAccountID, endpoint.targetSuccess())

				recorder := f.serve(t, endpoint.request(false))

				f.requireNoFallback(t, recorder, zhipuDSFallbackPrimaryAttempt)
				require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
			})
		}
	}
}

// 回退前按目标平台重新做配额准入：deepseek 平台日配额已用尽、或目标分组 RPM 超限时
// 不回退；配额充足时回退成功，并把用量记到 deepseek 平台配额与目标分组的 RPM 上。
func TestZhipuDeepSeekFallback_RechecksTargetQuotas(t *testing.T) {
	for _, endpoint := range zhipuDSFallbackEndpoints {
		for _, tc := range []zhipuDSFallbackCase{
			{
				name: "deepseek platform daily quota exhausted",
				mutate: func(f *zhipuDSFallbackFixture) {
					f.quotaRepo.record = f.quotaRecord(10, 10)
				},
			},
			{
				name: "target group RPM limit exceeded",
				mutate: func(f *zhipuDSFallbackFixture) {
					f.groups[zhipuDSFallbackTargetGroupID].RPMLimit = 5
					f.rpmCache.groupCounts = map[int64]int{zhipuDSFallbackTargetGroupID: 6}
				},
			},
		} {
			t.Run(endpoint.name+"/"+tc.name, func(t *testing.T) {
				f := newZhipuDSFallbackFixture(t, endpoint)
				tc.mutate(f)
				f.upstream.script(zhipuDSFallbackSourceAccountID, zhipuDSFallbackUpstreamError(http.StatusServiceUnavailable))
				f.upstream.script(zhipuDSFallbackTargetAccountID, endpoint.targetSuccess())

				recorder := f.serve(t, endpoint.request(false))

				f.requireNoFallback(t, recorder, zhipuDSFallbackPrimaryAttempt)
				require.Equal(t, http.StatusBadGateway, recorder.Code, recorder.Body.String())
			})
		}

		t.Run(endpoint.name+"/quotas within limits are charged to the target", func(t *testing.T) {
			f := newZhipuDSFallbackFixture(t, endpoint)
			f.quotaRepo.record = f.quotaRecord(10, 0)
			f.groups[zhipuDSFallbackTargetGroupID].RPMLimit = 5
			f.upstream.script(zhipuDSFallbackSourceAccountID, zhipuDSFallbackUpstreamError(http.StatusServiceUnavailable))
			f.upstream.script(zhipuDSFallbackTargetAccountID, endpoint.targetSuccess())

			recorder := f.serve(t, endpoint.request(false))

			f.requireFallsBack(t, recorder)
			_, quotaUsed, _ := f.cache.snapshot()
			require.InDelta(t, f.usageLogs.recorded()[0].ActualCost, quotaUsed[service.PlatformDeepseek], 1e-12,
				"the fallback must be charged to the user's deepseek platform quota")
			require.Equal(t, []int64{zhipuDSFallbackTargetGroupID}, f.rpmCache.countedGroups(),
				"only the target group's RPM counter may be charged")
		})
	}
}

func (f *zhipuDSFallbackFixture) requireNothingBilled(t *testing.T) {
	t.Helper()
	require.Empty(t, f.usageLogs.recorded(), "a request that never produced output must not be billed")
	require.Empty(t, f.billing.recorded(), "a request that never produced output must not be charged")
	f.requireNoLeakedReservation(t)
}

// 流式请求在首个输出之前失败：上游 HTTP 503，或（Responses 协议）仅收到
// response.created 这类生命周期帧后流内失败。此时客户端尚未收到任何字节，回退后客户端
// 拿到的是完整的 DeepSeek 流，来源的 response.id 不得泄漏。
func TestZhipuDeepSeekFallback_StreamFallsBackBeforeAnyOutput(t *testing.T) {
	for _, endpoint := range zhipuDSFallbackEndpoints {
		t.Run(endpoint.name+"/upstream HTTP 503", func(t *testing.T) {
			f := newZhipuDSFallbackFixture(t, endpoint)
			f.upstream.script(zhipuDSFallbackSourceAccountID, zhipuDSFallbackUpstreamError(http.StatusServiceUnavailable))
			f.upstream.script(zhipuDSFallbackTargetAccountID, endpoint.targetStreamSuccess())

			recorder := f.serve(t, endpoint.request(true))

			f.requireFallsBack(t, recorder)
			require.NotContains(t, recorder.Body.String(), endpoint.sourceStreamID)
		})
		if endpoint.streamMetadata == "" {
			continue
		}
		for _, failure := range endpoint.streamFailures {
			t.Run(endpoint.name+"/in-band "+failure.name+" after lifecycle frames only", func(t *testing.T) {
				f := newZhipuDSFallbackFixture(t, endpoint)
				f.upstream.script(zhipuDSFallbackSourceAccountID, zhipuDSFallbackSSEReply(endpoint.streamMetadata+failure.event))
				f.upstream.script(zhipuDSFallbackTargetAccountID, endpoint.targetStreamSuccess())

				recorder := f.serve(t, endpoint.request(true))

				f.requireFallsBack(t, recorder)
				require.NotContains(t, recorder.Body.String(), endpoint.sourceStreamID)
			})
		}
	}
}

// 上游以 HTTP 200 建流，但第一帧就是限流 / 过载失败事件：此时同样没有任何输出下发，
// 必须回退到目标模型，而不是把来源的错误帧转给客户端。
func TestZhipuDeepSeekFallback_StreamFallsBackOnInBandFailureBeforeAnyOutput(t *testing.T) {
	for _, endpoint := range zhipuDSFallbackEndpoints {
		for _, failure := range endpoint.streamFailures {
			t.Run(endpoint.name+"/"+failure.name, func(t *testing.T) {
				f := newZhipuDSFallbackFixture(t, endpoint)
				f.upstream.script(zhipuDSFallbackSourceAccountID, zhipuDSFallbackSSEReply(failure.event))
				f.upstream.script(zhipuDSFallbackTargetAccountID, endpoint.targetStreamSuccess())

				recorder := f.serve(t, endpoint.request(true))

				f.requireFallsBack(t, recorder)
				require.NotContains(t, recorder.Body.String(), endpoint.sourceStreamID)
			})
		}
	}
}

// 来源的响应 ID 或语义输出一旦已经下发给客户端，就不能再换模型：此后流内失败只
// 报告在原流上，目标账号不得被请求。
func TestZhipuDeepSeekFallback_StreamDoesNotFallBackOnceResponseIDOrOutputSent(t *testing.T) {
	for _, endpoint := range zhipuDSFallbackEndpoints {
		for _, stage := range []struct {
			name       string
			prefix     string
			withOutput bool
		}{
			{name: "id frame only", prefix: endpoint.streamIDOnly},
			{name: "semantic output already sent", prefix: endpoint.streamOutput, withOutput: true},
		} {
			for _, failure := range endpoint.streamFailures {
				t.Run(endpoint.name+"/"+stage.name+"/"+failure.name, func(t *testing.T) {
					f := newZhipuDSFallbackFixture(t, endpoint)
					f.upstream.script(zhipuDSFallbackSourceAccountID, zhipuDSFallbackSSEReply(stage.prefix+failure.event))
					f.upstream.script(zhipuDSFallbackTargetAccountID, endpoint.targetStreamSuccess())

					recorder := f.serve(t, endpoint.request(true))

					if endpoint.protocol == service.APIProtocolAnthropic && !stage.withOutput {
						// message_start is staged until content arrives, so its ID never reached the client.
						f.requireFallsBack(t, recorder)
						require.NotContains(t, recorder.Body.String(), endpoint.sourceStreamID)
						return
					}
					f.requireNoFallback(t, recorder, zhipuDSFallbackPrimaryAttempt)
					body := recorder.Body.String()
					// 前置条件：来源的响应 ID（以及语义输出）确实已下发。
					require.Contains(t, body, endpoint.sourceStreamID)
					if stage.withOutput {
						require.Contains(t, body, zhipuDSFallbackPartialAnswer)
					}
					// 失败必须报告在原流上，而不是静默截断。
					require.Contains(t, body, zhipuDSFallbackSourceErrorText)
				})
			}
		}
	}
}
