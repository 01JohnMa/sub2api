# 03 — 当前组织身份与隔离入口

**What to build:** 为组织管理员提供只代表其当前组织的 JWT 管理入口，并让前后端明确区分平台管理员、组织管理员、普通成员和无组织用户。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 02 — 平台成员分配与组织角色

**Status:** resolved

- [x] `auth/me` 返回可空组织摘要和组织角色，不改变现有平台 `role` 与 `isAdmin` 语义。
- [x] 当前组织 API 不接受客户端 organization ID，每次请求重新加载 User 和 Organization 当前状态并只允许 active `organization_admin`。
- [x] 旧 JWT 在成员移出、降级或组织停用后的下一次请求立即失权，普通成员和无组织用户不能进入组织管理接口。
- [x] 列表、详情、数量、分页 total、筛选和聚合全部携带服务端组织谓词；外国对象统一 404，权限不足统一 403。
- [x] 前端新增组织管理员路由守卫和导航，且不展示全局账号、Group、代理、设置、备份、运维、支付、个人余额/订阅、并发、RPM、平台 quota、API Key quota 或角色控制。
- [x] 当前组织 API 不提供组织资料/状态/角色、个人余额/订阅、并发、RPM、平台 quota 或 API Key quota 的 mutation；直接调用不存在的组织管理路由稳定失败。
- [x] 使用至少两个组织验证 IDOR、撤权、停用和直接访问受保护 URL 的行为。

## Comments

- Added active-organization-only frontend authority, server-derived current-organization scope tests, scoped pagination/aggregate assertions, foreign-member 404/authority 403 coverage, and runtime 404 checks for unsupported organization mutations.
- Verified focused Vitest (119 tests), handler/middleware/routes Go tests, and PostgreSQL-backed current-organization scope integration tests. Two read-only reviews found no Critical or Important issues; remaining Minor risk is that two frontend tests mirror guard/source structure instead of mounting the real navigation component.
