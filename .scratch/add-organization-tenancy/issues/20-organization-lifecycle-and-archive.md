# 20 — 组织停用、恢复与安全归档

**What to build:** 完成组织的非破坏性生命周期，使停用立即阻止新组织操作，并让已结清组织在明确冻结未分配余额后不可逆归档。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 11 — 成员移出与个人 Key 恢复; 19 — Pending reconciliation 操作台

**Status:** resolved

- [x] 平台管理员可以停用 active 组织和恢复 disabled 组织；状态转换本身不修改成员、Key、allocation、reservation 或历史金额。
- [x] Disabled 组织立即拒绝组织管理员入口和新组织 Key execution，但已 dispatch execution 仍可 settle、release 或 reconcile。
- [x] 停用、恢复或归档提交后执行 organization-wide auth/billing cache invalidation，并通过跨实例测试证明授权与资金正确性不依赖失效成功。
- [x] 平台 UI 提供 disable/reactivate 控件、当前状态刷新和二次确认，并正确处理 403、版本冲突和并发状态变化。
- [x] Archive 前所有成员 available/reserved 和 unresolved reservation 必须为零，否则返回稳定冲突且不改变组织状态或资金。
- [x] 平台 UI 显示将永久冻结的精确 unallocated 金额和不可恢复后果，并要求绑定当前组织/余额版本的二次确认；版本变化时刷新重试。
- [x] Archived 组织不可恢复、不可新增成员、角色变化或资金写；平台保留历史读，并可移出零 allocation 成员。
- [x] Disabled/archived 组织的现有组织 Key 在 billing、scheduling 和 upstream dispatch 前稳定拒绝，不能通过缓存、SimpleMode 或旧连接快照继续新 execution。
- [x] 并发测试覆盖 disable/archive 与 add/change/remove/reservation 竞争，证明状态提交后的 reservation atomic recheck 必然失败，且 archived 后不会提交新成员、角色变化或组织 execution。

## Comments

- Added transactional disable/reactivate/archive operations, version-bound archive confirmation, settled-balance checks, and best-effort organization-wide cache invalidation after commit.
- Added platform-admin lifecycle controls with stale-response protection and stable conflict/access handling.
- Verified focused Go tests, real PostgreSQL/Testcontainers lifecycle and cache-failure tests, frontend tests/typecheck/ESLint, and final Spec/Standards reviews with no Critical or Important findings.
