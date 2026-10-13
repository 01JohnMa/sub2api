# 22 — 成员用量与安全 Key 元数据

**What to build:** 让组织管理员查看当前成员的请求、Token、费用和安全 API Key 元数据，并用一个不可操作的已离开成员桶保持历史组织总量可对账。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 21 — 组织概览与财务报表

**Status:** resolved

- [x] 当前成员行按 immutable organization/allocation/user snapshot 聚合 request count、Token 和 8 位 settled cost。
- [x] 已离开或已删除成员的 evidence 进入一个不可操作的 departed-member 历史桶，不重新暴露已删除账号或 Key。
- [x] 当前成员行加 departed bucket 必须分别与组织 request count、Token 和 cost 总量对账。
- [x] 成员、allocation 和 API Key 精确筛选必须属于当前组织，外国对象返回 404 且不影响 totals。
- [x] API Key 只返回 ID、名称、状态、成员、组织、创建/最后使用/过期时间、quota 和 quota_used。
- [x] DTO 不包含完整 Key、ACL、凭据、request/error payload、个人余额或任何可恢复 Secret；组织 API 不提供 Key mutation 路由。
- [x] 前端覆盖日期过滤、空态、departed bucket、Secret-free rendering、撤权和 403/404 恢复。

## Comments

- Added current-member/departed-member reporting filters and paginated secret-safe API Key metadata without mutation routes.
- Added organization-admin UI pagination, access-loss clearing, and generation guards for member, report, ledger, and Key requests.
- Verified real PostgreSQL organization-scope tests, 68 target frontend tests, typecheck, ESLint, formatting/diff checks, and final Spec/Standards reviews with no Critical or Important findings.
