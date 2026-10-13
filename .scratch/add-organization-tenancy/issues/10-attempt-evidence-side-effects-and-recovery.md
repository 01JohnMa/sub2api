# 10 — Attempt evidence、side effect 与恢复基座

**What to build:** 在结算核心之上完成多 attempt 的 provider evidence、Account cost side effect、provenance 冲突、overage 和崩溃恢复，使后续 endpoint adapter 只需提供成本上界和协议映射。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 09 — Execution、reservation 与 finalization 核心

**Status:** resolved

- [x] 每次 dispatch 使用服务端 attempt UUID，并保存 Account/route/pricing snapshot、provider request/correlation ID、Token/raw cost、observed time 和 provenance hash。
- [x] Attempt/provider evidence 与唯一 Account side-effect outbox/apply-state 同事务提交，状态支持 pending、applied、deleted-target no-op 和 operator-attention。
- [x] Worker 可安全 redrive evidence-committed-before-posting 崩溃，最终每个有 usage 的 attempt 只有一条 Account-cost posting 和至多一次 live quota update。
- [x] API Key、User 或 Account 在 side effect 应用前被删除时，目标更新成为 audited terminal no-op，不重试永久失败，也不回滚组织结算。
- [x] 服务端固定 adapter evidence 高于匹配 provider-query evidence 的 provenance precedence；成本、Token 或终态冲突保持 pending incident，管理员不能择优。
- [x] Raw cost 超过 reservation ceiling 时组织最多承担预留额，差额只作为同一 Account posting 的 platform-overage 分类，不创建第二笔 debit。
- [x] Post-dispatch 未知结果进入 pending reconciliation，年龄本身不能释放；可信 proven-zero 才允许 release。
- [x] 使用 fake multi-attempt adapter 覆盖 failed failover、断流、崩溃、conflicting provenance、overage、deleted-target no-op 和 redrive；真实 endpoint spending 仍关闭。

## Comments

- Added forward migration `157_organization_execution_recovery.sql`, Ent schemas, immutable attempt Account postings/effects, provenance/overage incidents, and scoped reconciliation audits. Migration 156 remains unchanged.
- Added multi-attempt orchestration, explicit post-dispatch unknown handling, fixed adapter-direct-over-provider-query precedence, rule-computed JWT-only reconciliation, scoped idempotency, Account/outbox redrive, deleted-target terminal no-op, and reservation-ceiling overage classification.
- Verified with full backend tests, targeted race tests, `go vet ./...`, strict OpenSpec validation, and real PostgreSQL migration/integration tests including failover sequence 2, evidence-before-posting recovery, Account deletion locking, deleted API Key/User/Account no-op, provenance conflict, historical reconciliation time, duplicate/scoped-key reconciliation, and overage replay.
- Read-only Standards and Spec reviews found no remaining Critical or Important issues after fixes.
- Real endpoint organization spending remains disabled. OpenSpec tasks 4.6 endpoint/WebSocket admission portions and the broader 4.10 endpoint matrix remain open for later tickets.
