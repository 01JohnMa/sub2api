# 09 — Execution、reservation 与 finalization 核心

**What to build:** 建立与具体 endpoint adapter 解耦的最小持久结算核心，使一个单 attempt fake execution 通过最小 immutable attempt evidence 完成安全预留、final evidence、结算或释放；多来源 provenance、Account side effect、多 attempt 和恢复机制由下一票完成。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 07 — 自定义额度追加与显式收回; 08 — 组织 Key 权威解析与 admission 门禁

**Status:** resolved

- [x] 每个可信组织 execution 和单次 fake dispatch 使用服务端 UUID，客户端 request ID 仅作元数据。
- [x] Reservation 持久化 organization/allocation、candidate/pricing snapshot、上界和 fingerprint；从权威 active-state read 持兼容锁到 commit，或在同一 atomic conditional write 中重验 Organization active/version、Key enabled/owner/organization、User non-deleted/organization、allocation active/user/organization/version，任一变化生成零费用 error evidence 且不创建 reservation。
- [x] 单 attempt fake adapter 在 terminalization 前持久化 immutable attempt evidence，至少包含 Account/route/pricing snapshot、服务端 correlation、固定 provenance、Token/raw cost 和 observed time。
- [x] 每个已经 terminalized 的可信 execution 恰好一条由 attempt evidence 或持久零费用证明派生的 final execution evidence；pending-reconciliation 在 resolution 前不得提前生成 final evidence，正费用必须引用 final attempt，零费用/non-billable/pre-dispatch error 不创建消费 posting。
- [x] Terminalization 原子提交 reservation 终态、final evidence、可选唯一 consumption posting、usage/error outbox 和 final cost/Token-dependent quota side-effect records。
- [x] `occurred_at` 固定为 execution 最终 outcome 首次确定时间；延迟 reconciliation、outbox 或投影不移动报表时间。
- [x] 同一 execution 的重复或冲突 settlement/release 具备唯一约束和幂等结果，任何 bucket 不为负，已结算金额不再属于 available/reserved 且不能被后续 reclaim 读取。
- [x] 以单 attempt fake adapter 验证成功、零费用、pre-dispatch error、低于 reservation 结算和重试；真实 endpoint spending 仍关闭。

## Comments

- 已完成单 attempt execution/reservation/finalization 核心；execution、correlation、attempt、evidence、reservation 和消费 posting 均使用服务端持久身份，客户端 request ID 仅保留为元数据。
- Reservation 事务持有 Organization `FOR SHARE` 到提交，并按 Organization → User/Key/allocation 及 allocation → reservation → execution 的稳定顺序重验当前归属；`balance_version` 仅作为审计快照，不作为资金 CAS。
- Attempt replay 比较完整不可变证据；Account snapshot 在 live Account 删除/软删时保留而 live ref 可空。Final evidence 增加 JSON proof 形状、attempt 派生字段、reservation/execution 终态一致性和正费用一对一消费 posting 约束。
- 已运行：`go generate ./ent`、`go generate ./cmd/server`、`go test ./migrations`、定向 service/repository tests、`go vet ./...`、完整 `go test ./...`、service/repository race tests，以及 Docker PostgreSQL integration（含 JSON null proof、未终态 reservation、篡改 attempt cost、额度不足 proof 回归），均通过。
- Account side-effect worker、多 attempt/failover、provenance reconciliation 和 crash recovery 仍由 Ticket 10 及后续票实现；组织 spending capability 仍保持默认关闭，未连接真实 gateway endpoint。
