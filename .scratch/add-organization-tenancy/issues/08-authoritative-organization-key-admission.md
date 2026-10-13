# 08 — 组织 Key 权威解析与 admission 门禁

**What to build:** 在任何计费或上游调度前权威识别组织 Key，并重新执行平台已有状态、quota、rate、RPM 和并发限制，确保错配或依赖异常绝不降级为个人请求。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 02 — 平台成员分配与组织角色

**Status:** resolved

- [x] 每个 API Key 请求从持久状态读取 Key、owner User、Organization 和 active allocation，不以缓存快照作为授权事实。
- [x] Key/User/allocation 的 user ID 和 organization ID 必须四方一致；缺行、空值错配、inactive allocation 或外国 allocation 均 fail closed 并产生无 Secret 告警。
- [x] 只有 Key 与 User 都权威为 null organization 且不存在 active allocation 时才进入现有个人 Key 逻辑。
- [x] disabled、archived、缺失组织或组织计费依赖异常在 billing、scheduling 和 dispatch 前稳定拒绝，SimpleMode 不绕过。
- [x] 在首个 pre-dispatch 组织 admission 边界调用 Ticket 01 的 capability registry；未通过 family/request-shape/ceiling/rollout gate 时返回稳定错误，且绝不回退个人计费。
- [x] 每个 HTTP execution 重新执行 User、Key、Group、RPM、API Key rate/quota、user-platform quota 和 concurrency admission guards。
- [x] 权威组织、用户、Key 和 allocation 快照进入可信服务端上下文，客户端 header/query/body 中的组织值对授权、计费和遥测无效。
- [x] 组织 spending 在本票完成后仍保持 capability gate 关闭。

## Comments

- 已完成 OpenSpec 4.1：Standard、SimpleMode、Gemini 与 Antigravity HTTP 入口均绕过 auth cache，权威加载 Organization/active allocation，并将无 Secret 快照写入服务端 context。
- capability matrix 已在 API Key middleware 的首个 pre-dispatch 边界接线；所有 ceiling definition 仍为 false，任何组织 Key 均不会进入个人余额或订阅路径。
- Google auth 已补齐标准 auth 的 IP ACL、专属 Group、运行时过期和 API Key quota guard。
- 新增组织专用 `CheckOrganizationPlatformEligibility`，对 billable HTTP execution 复用 user-platform quota、API Key rate 和 RPM，但不读取个人余额或订阅；SimpleMode、零 allocation 和必需依赖异常均 fail closed。
- Messages、Responses HTTP、Chat Completions、Embeddings、Images、Gemini/Antigravity 的 9 个主 HTTP 入口在 user concurrency 获取后通过可信组织 context 选择该路径；个人 Key 保持现有 `CheckBillingEligibility`。
- `count_tokens`/models/usage 仍由 Ticket 17 处理，Responses WebSocket 每 turn 仍由 Ticket 13 处理；prompt-too-long 内部 fallback 对组织请求保持关闭，直到 execution/candidate snapshot 能安全复用同一 admission/reservation。
- Standard 与 Google middleware 在任何个人 subscription/balance 路径前保留显式组织 fail-closed；所有 spending capability 和 cost ceiling 仍关闭。
