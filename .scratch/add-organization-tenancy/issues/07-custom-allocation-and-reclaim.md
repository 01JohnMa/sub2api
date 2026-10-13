# 07 — 自定义额度追加与显式收回

**What to build:** 让组织管理员按成员自定义追加额度或显式收回未消费额度，并让平台管理员在更高权限边界执行同类操作，完整交付预览、确认和冲突恢复界面。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 06 — 一键平均追加未分配额度

**Status:** resolved

- [x] 自定义 preview/execution 只接受一个或多个 eligible 普通成员的正数追加额，重复、外国、过期、超池或非法金额使整单回滚。
- [x] 组织管理员只能操作本组织普通成员；交互式平台管理员 JWT 在 active 组织可操作任意当前成员，在 disabled 组织只能显式收回，长效 Admin API Key 对所有资金写返回 403。
- [x] Reclaim 只能转回当前 available 且未 reserved 的金额，不允许负数、目标余额语义、已消费改写或隐式减记。
- [x] Preview/execution 使用规范金额、短期 token 和组织/操作作用域幂等键；重放先重新验证当前授权，仍有权限且负载相同才返回原结果，stale、权限变化或版本变化不部分执行，已撤权 actor 不得读取历史资金结果。
- [x] 前端同时提供自定义 before/delta/after 预览、显式收回控制和确认结果，并覆盖 stale、403、404、冲突与重试恢复。
- [x] 自定义追加和收回具备并发资金守恒、跨组织隔离、自我/管理员目标拒绝和幂等回归测试。

## Comments

- 平台管理员 UI 支持 active 组织 custom/reclaim、disabled 组织 reclaim-only，并对 archived 组织保持只读。
- 前端按 preview fingerprint 复用幂等键，stale-preview 会清除旧确认并要求重新预览。
