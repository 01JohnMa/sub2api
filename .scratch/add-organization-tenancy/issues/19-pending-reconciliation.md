# 19 — Pending reconciliation 操作台

**What to build:** 为平台管理员提供 secret-safe 的 pending-reconciliation 列表、详情和受限 resolution，使未知 execution 只能按服务端可信证据结算或 proven-zero 释放。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 10 — Attempt evidence、side effect 与恢复基座

**Status:** resolved

- [x] 平台管理员只读边界的 pending 列表/详情显示 reservation、candidate ceiling、attempt outcomes、可信 evidence references 和服务端 allowed resolution；现有 Admin API Key 可读但不能 mutation。
- [x] 只有交互式平台管理员 JWT 可以执行 resolution；长效 Admin API Key 和组织管理员返回 403。
- [x] 平台管理员只能确认服务端计算的 settle 或 proven-zero release；来源冲突时无操作按钮并保持 pending incident。
- [x] Resolution 原子生成唯一 final evidence、可选 consumption posting、reservation 终态、内部/public reason audit、Account side-effect outbox 和 usage/error outbox，不接受证据上传、任意金额或非 pending mutation。
- [x] 组织管理员只看到本组织脱敏 pending count/status，不能查看 provider evidence、平台内部原因或执行 reconciliation。
- [x] 平台 UI 提供 secret-safe evidence 摘要、服务端计算结果、冲突只读态和确认操作，并正确处理幂等重放与权限变化。
- [x] 自动与人工恢复测试覆盖 exact settle、proven-zero、来源冲突、overage、重复确认、Admin API Key 403 和跨组织隔离。

## Comments

- Added organization-scoped pending list/detail/summary APIs, JWT-only idempotent resolution, secret-safe attempt/evidence DTOs, production DI, platform reconciliation UI, and the organization-admin count/status view.
- The reconciliation transaction now rejects any execution with an attempt that lacks immutable evidence, including a later dispatched attempt after an earlier retryable result; this prevents direct API calls from settling an unknown final outcome.
- Verified focused Go service/handler/routes/cmd tests, frontend Vitest (45 tests), `vue-tsc`, diff/gofmt checks, and PostgreSQL/Testcontainers reconciliation tests including cross-organization isolation, provenance conflict, immutable audit, and the unknown-later-attempt financial guard. Final Spec and Standards reviews found no Critical or Important issues.
