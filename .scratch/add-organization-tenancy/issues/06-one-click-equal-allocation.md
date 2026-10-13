# 06 — 一键平均追加未分配额度

**What to build:** 为组织管理员提供一次预览、一次确认的一键分配，把全部未分配额度平均追加给当前 active 普通成员，同时保留舍入余数。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 04 — 平台 JWT 授信与不可变账本; 05 — 组织管理员直接加入普通成员

**Status:** resolved

- [x] Preview 返回当前池余额、eligible/excluded 成员快照、每人 before/delta/after、总额、8 位向下舍入余数和 resulting pool。
- [x] Eligible 集合只包含当前 active `organization_member`，调用者和其他组织管理员始终排除。
- [x] Execution 必须携带短期 preview token、匹配的规范负载和幂等键；池、成员、角色、状态或版本变化使整单 stale，不部分执行。
- [x] 每人获得相同的向下舍入追加额，现有 allocation 不覆盖，余数继续留在未分配池。
- [x] 零池、无 eligible 成员或每人追加额低于精度时返回稳定 no-op，且不写无意义资金 posting。
- [x] 已提交成功的相同幂等请求先重新验证当前组织管理员/平台管理员授权；仍有权限时即使 preview 过期也返回原结果，不发生第二次分配，已撤权 actor 返回当前 403/404 而非历史资金响应。
- [x] 前端完整展示预览差异、排除成员、余数和确认结果，并能从 stale-preview 冲突重新获取预览。

## Comments

- no-op 以 scoped operation envelope 持久化幂等结果，不创建零额 posting，也不进入账本列表或改变 balance version。
- PostgreSQL integration 覆盖并发 allocation/reclaim 串行与资金守恒。
