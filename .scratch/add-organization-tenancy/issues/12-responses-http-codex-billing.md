# 12 — Responses HTTP 与 Codex HTTP aliases 计费

**What to build:** 把 Responses HTTP 及其 Codex HTTP aliases 作为第一组真实组织计费 adapter 接入完整 execution/evidence 基座；WebSocket 与直接 Chat Completions 由后续独立票完成。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 11 — 成员移出与个人 Key 恢复

**Status:** resolved

- [x] Responses HTTP 支持矩阵逐个声明允许的 request shape、stream/tools、输出上限、模型映射、service tier、倍率和 failover ceiling。
- [x] 缺少客户端输出上限时只使用已验证的服务端/模型上限；无法安全计算时在 dispatch 前稳定拒绝。
- [x] 真实 dispatch 只能使用 reservation candidate snapshot 内的候选，正常、断流、超时、proven-zero 和 failed failover 均通过统一 terminalization。
- [x] `/v1/responses`、`/responses` 和 Codex HTTP aliases 使用同一 candidate/pricing snapshot、attempt evidence 和 final evidence，不创建别名专属 reservation 或 debit。
- [x] 需要 Chat compatibility fallback 的 request shape 在票 14 完成前保持 capability-gated，不通过内部转换绕过组织门禁。
- [x] 组织 Key 不读取或扣减个人余额/订阅，平台 Account、API Key quota、user-platform quota、rate、RPM 和 concurrency 仍按 attempt/execution 口径生效。
- [x] Responses HTTP family 自身 adapter 测试通过后只可在测试或非生产 capability gate 中启用；生产 spending 默认关闭并由 23 单独控制。

## Comments

- Added one frozen Responses candidate/pricing plan for `/v1/responses`, `/responses`, and `/backend-api/codex/responses`, with verified model ceilings, bounded input shapes/tools, mapped-model revalidation, service-tier policy, multiplier snapshots, and bounded failover.
- Organization admission now reserves before consuming RPM or user concurrency. Capability, malformed-body, unsupported-subpath, moderation, and other pre-dispatch rejections create zero-cost final evidence after trusted scope is established.
- Dispatch records validated usage only; missing usage, transport ambiguity, and incomplete streams remain pending reconciliation instead of releasing funds. Unsupported SetupToken candidates are excluded, while deterministic token/request-construction failures terminalize as proven zero.
- Account attempt effects and positive-cost API Key/user-platform quota outboxes have request-driven idempotent redrive. The API-key outbox atomically applies quota and rate-window usage, and personal balance/subscription billing remain absent.
- Verified targeted tests, full `internal/service`, `internal/handler`, and `internal/repository` package tests, unit-tag organization admission/capability tests, formatting/diff checks, strict OpenSpec validation, and two read-only reviews with no Critical or Important findings. WebSocket, direct Chat compatibility, and production rollout remain owned by tickets 13, 14, and 23.
