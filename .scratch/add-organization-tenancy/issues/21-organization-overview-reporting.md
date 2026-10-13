# 21 — 组织概览与财务报表

**What to build:** 为组织管理员提供范围正确、可对账的组织概览、趋势和财务视图，所有请求、Token 与费用使用唯一 final evidence 和统一时间锚。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 12 — Responses HTTP 与 Codex HTTP aliases 计费; 19 — Pending reconciliation 操作台

**Status:** resolved

- [x] 概览按半开 `[start, end)` 和 final evidence `occurred_at` 聚合，不使用投影写入时间或 reconciliation 时间。
- [x] `request_count = success + error`，同一 execution 只计一次；billable disconnect 只计一个 error，`count_tokens` 不进入 `request_count`、`success_count`、`error_count_total`、member request total 或 SLA 公式。
- [x] `business_limited_count` 是 error 子集，`error_count_sla` 只包含 `is_business_limited = false` 的 error，业务限流不得进入 SLA 分母。
- [x] 展示输入、输出、cache creation、cache read Token、model distribution、趋势和当前成员数；期间 active-member count 定义为 `[start,end)` 内排除 `count_tokens` 后 final evidence 的 distinct allocation/user identity snapshot。
- [x] Token totals 来自 final evidence 的四类 Token 字段并与成员汇总对账；`count_tokens` evidence 的 Token 仍按该口径保留，但不影响请求、active-member 或 SLA 计数。
- [x] `success_rate = success_count / (success_count + error_count_sla)`、`error_rate = error_count_sla / (success_count + error_count_sla)`，分母为零时两者均返回 0；不能替换为 success/total 的普通公式。
- [x] Cost 使用 8 位 organization settled amount，并与每条正费用 evidence 的唯一 consumption posting 对账；零费用 evidence 无 posting。
- [x] 财务摘要同时展示当前 unallocated、available、reserved 快照与累计已结算 `consumed`，并单独按所选时间范围展示 final-evidence cost；不从 legacy float usage 或当前池反推历史费用，也不混淆快照与期间值。
- [x] 组织管理员看到的 public ledger 只包含公开 operation label/public reason 和脱敏 actor display，不包含 internal audit reason、actor ID、Account、provider/evidence detail 或 Secret。
- [x] 概览包含组织管理员允许看到的 redacted pending count/status，不包含 provider evidence 或平台内部原因。
- [x] 空数据返回零值/空序列，不回退全局 dashboard cache；双组织测试证明所有 total、bucket、facet 和分页均隔离。

**Completion notes:**

- Final evidence reporting, current-member/departed-member usage, platform and current-organization report APIs, safe public ledger pagination, and overview UI are implemented.
- Verified with focused Go repository/service/handler/routes/migration tests, real PostgreSQL/Testcontainers reporting coverage (scope/time boundaries, SLA, four token classes, model, posting reconciliation, empty data, late reconciliation), frontend typecheck, 32 target Vitest tests, ESLint, gofmt, and `git diff --check`.
- Full service/handler package runs can still hit unrelated sandbox IPv6 `httptest` listener failures; the Ticket 21-focused tests pass.
