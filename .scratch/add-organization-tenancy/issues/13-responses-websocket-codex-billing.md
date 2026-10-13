# 13 — Responses WebSocket 与 Codex WS 按 turn 计费

**What to build:** 把 Responses WebSocket 及 Codex WS aliases 接入组织计费，使每个 `response.create` 都是新的权威 execution，连接本身不能缓存旧授权、旧额度或平台限制结果。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 12 — Responses HTTP 与 Codex HTTP aliases 计费

**Status:** resolved

- [x] 每个 `response.create` 重新加载 Key/User/Organization/allocation，重新执行 User、Key、Group、API Key quota/rate、user-platform quota、RPM 和 concurrency guards，并生成新的 execution UUID、reservation 与 candidate snapshot。
- [x] 成员移出、组织停用、Key 禁用、平台限制变化或额度耗尽发生在两个 turn 之间时，下一个 turn 在 dispatch 前拒绝，前一 turn 的 evidence 与结算不改写。
- [x] `/v1/responses`、`/responses` 与 Codex WS aliases 的每-turn 组织归属、ceiling、stream、tools 和 failover 行为一致，不创建连接级资金快照。
- [x] 正常完成、部分流、下游断开、超时、上游错误、proven-zero 和 pending recovery 都通过统一 terminalization，不泄漏 reservation 或重复结算。
- [x] 同一连接的并发或重试 turn 以 execution ID 幂等计入平台 guards 与组织结算，且不能复用另一 turn 的 candidate/pricing snapshot。
- [x] Responses WS family 通过每-turn 状态变化、断流和并发测试后只可在测试或非生产 gate 中独立启用；生产默认关闭。

## Comments

- Added per-turn authoritative admission and durable multi-attempt execution while preserving the parent execution across reconnects.
- Frozen candidates now drive the first account and every retry in order; pre-dispatch connection failures release with zero-cost evidence, while uncertain first-frame failures remain pending reconciliation.
- Verified focused and full service/handler suites, relay/service/handler race tests, formatting, diff checks, and final Spec/Standards reviews with no Critical or Important findings.
