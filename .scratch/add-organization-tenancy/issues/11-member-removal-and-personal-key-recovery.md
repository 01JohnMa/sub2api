# 11 — 成员移出与个人 Key 恢复

**What to build:** 通过一个可审计的原子事务移出成员、自动回收剩余组织额度并把现有 Key 恢复为个人 Key，同时封堵 User 删除旁路和并发竞态。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 07 — 自定义额度追加与显式收回; 10 — Attempt evidence、side effect 与恢复基座

**Status:** resolved

- [x] 成员移出遇任何 unresolved 或 pending-reconciliation reservation 立即返回稳定冲突，不在 HTTP 请求中等待，也不改变成员或资金。
- [x] 无未决 reservation 时，移出事务把全部 available 原子转回同一组织 unallocated pool、关闭 allocation、清空 User/API Key 组织归属，并保留已消费历史、Key Secret/状态、Group、quota、限流和个人配置；个人余额、个人订阅和支付订单绝不改变。
- [x] 组织管理员只能移出本组织普通成员；平台管理员可移出组织管理员并可从 archived 组织移出零 allocation 成员，外国 ID 返回 404。
- [x] 平台管理员执行会自动 reclaim 的移出只接受交互式 JWT，长效 Admin API Key 对该资金写返回 403；所有移出使用作用域幂等键，重放先重新认证并校验当前 scope/role/lifecycle，已移出或降级 actor 返回当前 403/404 而非历史资金响应。
- [x] 平台删除、OAuth cleanup 和 soft-delete 入口都在 User 锁内拒绝仍有组织归属或 allocation 的用户，且不级联删除历史财务证据；产品当前未暴露自助删除路由，未来入口仍必须复用同一 UserRepository/Ent guard。
- [x] 移出提交后按用户和 Key 失效 auth/billing cache；跨实例测试证明新请求看到个人 Key 合同，同时在途请求仍按原 allocation 完成且资金不被消费和收回两次。
- [x] 前端显示将回收的 available 金额和 Key 恢复后果，覆盖受保护管理员、pending reservation、stale、403/404/conflict 与幂等重试。

## Comments

- Added a real Redis/PostgreSQL integration test with two APIKeyService instances: member removal publishes invalidation, the second instance's L1 cache is cleared by its subscriber, and the next request reloads the personal Key contract.
- Added positive-cost settlement/removal concurrency coverage. The frozen allocation remains attached to final evidence and the consumption posting; settlement consumes `0.25`, removal reclaims `1.75`, and repeated terminalization/removal produces one consumption posting and one reclaim operation.
- Verified targeted service unit tests, Redis/PostgreSQL repository integration tests, and `git diff --check`. Final Spec and Standards reviews found no Critical, Important, or blocking Minor issues.
