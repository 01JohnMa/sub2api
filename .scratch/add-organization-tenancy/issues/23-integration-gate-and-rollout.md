# 23 — 全链路门禁与分阶段 rollout

**What to build:** 用跨组织安全、资金守恒、全 adapter 和前端身份矩阵验证整个组织能力，并提供默认关闭、可分阶段启用且可安全回滚的 rollout；本票不自行执行生产启用。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 13 — Responses WebSocket 与 Codex WS 按 turn 计费; 14 — Chat Completions compatibility 组织计费; 15 — Messages 组织计费 adapter; 16 — Embeddings 与 Images 组织计费; 17 — 非计费端点与 Token probe; 18 — Gemini 与 Antigravity 组织计费 adapter; 20 — 组织停用、恢复与安全归档; 22 — 成员用量与安全 Key 元数据

**Status:** in-progress

- [ ] 运行 schema/migration、领域服务、repository、middleware、handler、Gateway adapter 和前端身份/组件的目标测试及完整后端测试套件。
- [ ] 运行前端 lint、typecheck、单元测试和生产 build，四类身份导航与后端权限矩阵一致。
- [ ] 并发压力测试覆盖授信、平均/自定义分配、收回、reservation、settlement、pending、移出、停用和归档，资金不变量始终成立。
- [ ] 两个以上组织的 IDOR、分页 totals、过滤、ledger、成员、Key metadata 和报表测试证明无跨组织泄露。
- [ ] 每个 adapter 验证 ceiling、candidate freeze、attempt Account cost、execution 单次组织扣费、断流、崩溃恢复、disable-vs-reservation 线性化和 side-effect redrive。
- [x] 所有 organization spending capability 在部署后仍默认关闭，并可按 endpoint family 独立启用和回退。
- [ ] Smoke plan 覆盖个人 Key 不受影响、组织授信/分配、组织 Key 结算、Key 转换、停用/恢复、对账和归档。
- [x] Rollback 文档明确：存在组织资金或 Key 后，旧版本回滚前必须停止新 execution、禁用组织 Key、结清/对账 reservation；实际生产操作需要单独用户授权。
- [ ] 运行 `openspec validate add-organization-tenancy --strict` 并检查最终 Git diff/status，确认所有变更映射到权威合同且没有混入用户无关文件。
- [ ] 完成最终只读安全 review，Critical/Important 清零，并确认没有修改 CPA OAuth 轮询、Group 路由或个人支付订单语义。

## Comments

- 2026-07-31：`openspec validate add-organization-tenancy --strict` 通过；rollout runbook 已补齐控制面、身份、生命周期 smoke、资金守恒查询和旧版本回滚前的 durable work 排空条件，但尚缺 rule-computed settle、proven-zero release、冲突 incident、幂等 resolve 及 resolve 后 outbox 排空的完整 reconciliation smoke，故 smoke 门禁保持未勾选。
- 后端完整默认测试套件曾在恢复出的完整临时树中通过；组织相关 `-race` 测试通过，但命令未带 `-tags=integration`。新增 `organization_rollout_invariant_integration_test.go` 已通过 `gofmt`/空白检查，cleanup 已改为单事务且中途失败整体回滚；该测试仍未确定性制造事务交错，只检查最终资金方程，且因完整临时树被移除尚未运行 PostgreSQL integration 或 `-race -count=10`，因此完整验证和压力门禁保持未勾选。
- 前端已有 AppHeader `4/4`、管理端组织视图/API `31/31`、组织端 API/视图 `37/37` 的目标测试证据；当前 production build、lint、typecheck 和全量单测无法在 dataless 工作区重跑，历史 build 早于 Ticket 20-22，故前端全量门禁保持未勾选。
- 双组织 IDOR、报表和 Key metadata 测试存在历史通过证据，但对应当前测试文件为 iCloud dataless，无法读取或重跑，且 tracker 无可审计命令记录，故该门禁恢复为未勾选。
- 原硬阻塞一（客户端 evidence upload）已修复：公开 evidence POST 已删除，内部 adapter/reconciler ingestion 未删除。
- 原硬阻塞二（usage/error outbox 无消费者）已修复：pending scanner 和 redrive 已覆盖两类 projection，并在同一事务中更新 projection 后再落 outbox apply 状态；真实 PostgreSQL redrive 仍待工作区水合后验证。
- 当前 `.git` 元数据不可读，`git status --short` 返回 `not a git repository`；大部分 tracker 文件为 iCloud dataless，官方 frontier 脚本读取时返回 `TimeoutError`。最后一次成功的镜像计算为 `valid_graph: true`、无 warnings、22 个 resolved、仅 Ticket 23 位于 frontier；本轮唯一已知 tracker 状态变化是 Ticket 23 从 ready 转为 `in-progress`，因此只能推导预期为 `dependency_ready=1`、`frontier=0`、`waiting_status=1 (Ticket 23)`，不能把该推导伪装成当前脚本重算结果。
- 未执行生产启用、部署、数据库变更、提交、暂存、推送或 PR 操作。
- 2026-07-31：已移除平台 pending reconciliation 下的客户端 evidence upload 路由、DTO、handler 和正向测试；保留内部 adapter/reconciler evidence ingestion 与 list/detail/resolve。当前 scoped `rg` 仅剩路由不存在的负向断言。
- 2026-07-31：已在 execution outbox redrive 中纳入 `usage_projection`/`error_projection`，projection 归属只取持久 execution/final evidence；client request ID 仅保留为 metadata，不再改写任何旧 personal/pre-auth usage/error 行；usage 缺少可信 execution projection 时写入 `organization:<execution_id>` synthetic request ID，error 写 execution-scoped fallback，并允许同 execution 多条精确 error projection。只有锁定真实 User、API Key 或 Account 后确认已删除才进入 `applied_noop_deleted_target`；非法 immutable snapshot 或正费用缺失 Account 保持 pending，合法零费用无 Account 证据按普通 `applied` no-op 结束，归因冲突/插入失败保持可重试 pending。
- 2026-07-31：新增 `160_organization_telemetry_attribution.sql`，只增加 nullable `organization_id`/`execution_id` 与普通 organization/time/execution lookup indexes；没有历史回填、没有 projection execution 唯一约束，以兼容可能按 `created_at` 分区的 `usage_logs` 和同 execution 多条 ops error projection。迁移字符串回归测试在隔离 harness 通过。
- 2026-07-31：新增 projection sqlmock 覆盖 fallback、synthetic request ID、重复 redrive、pending scanner、多 ops error、删除目标、半归因冲突和插入失败；当前未能在真实工作区运行，原因是 `backend/go.mod` 及多个依赖文件 iCloud `dataless` 读取超时。OpenSpec strict 已通过；任务 1.4/4.9 仍未勾选，因 Ent/service model 与所有 telemetry writers 尚未完成全链路闭环。
- 2026-07-31：最终只读 review 无 Critical/Important；已修正非法 immutable snapshot 被误标为 deleted-target 的边界。projection 测试仍缺真实 PostgreSQL 双 worker 并发 redrive、重复 outbox/event 和事务回滚后重试证据；Ticket23 继续保持 `in-progress`，不宣称全链路完成。
- 2026-10-13（文档整理）：工作区已恢复可读写、git 元数据正常，此前 2026-07-31 记录的 iCloud dataless / git 不可读环境限制已不再成立；本 ticket 保持 `in-progress`，全部未勾选门禁（后端完整套件、前端全量四件套、并发压力、IDOR 矩阵、adapter 全验证、reconciliation smoke、strict validate 复跑、最终安全 review）维持未勾选，待实际执行后再勾，不在整理时伪造成完成。
