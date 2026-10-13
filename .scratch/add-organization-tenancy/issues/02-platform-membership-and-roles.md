# 02 — 平台成员分配与组织角色

**What to build:** 让平台管理员能够把无组织用户加入组织并任命组织管理员，同时保证平台角色、组织角色、零额度 allocation 和现有 API Key 归属在一个事务内保持一致。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 01 — 平台组织基础与 capability 门禁

**Status:** resolved

- [x] 平台管理员可以把未删除且无组织的注册用户加入 active/disabled 组织，并选择 `organization_member` 或 `organization_admin`。
- [x] 平台管理员可以在同一组织内切换组织角色，且不修改用户现有的全局 `admin/user` 平台角色。
- [x] 加入组织会在同一事务创建新的零 available/zero reserved allocation，并把所有未删除 API Key 转为该组织归属。
- [x] User、API Key 和 allocation 的单组织及组织角色约束由数据库和服务事务共同保证，重复或跨组织加入稳定失败且不泄露目标组织。
- [x] API Key 创建、成员写入以及平台删除、自助删除、OAuth cleanup、soft-delete 四类 User 删除路径在同一 User 锁协议下串行；直接 Ent 删除/设置 `deleted_at` 默认拒绝，无事务平台删除 fail-closed。
- [x] 成员加入或组织角色变更提交后 best-effort 失效 auth cache；gateway 每次权威重载 Key/User 归属，并在 Ticket 08 前拒绝全部组织 Key，因此正确性不依赖缓存失效成功。
- [x] 平台 UI 可以分配成员和任命组织管理员；归档组织的新增和角色变化保持不可用。

## Comments

- 当前没有组织余额 billing cache；成员提交后会按用户枚举 Key、清除 L1/L2 auth cache 并发布跨实例失效。失效是性能/收敛优化，gateway 不使用缓存授权当前组织归属。
- 完整组织计费 resolver 尚未完成；当前组织 Key 在 Standard/SimpleMode 均统一返回 `ORGANIZATION_SPENDING_DISABLED`，不会回退个人资金。
- 成员移除、额度回收和 Key 恢复个人归属仍由 Ticket 11 实现，本票据不提前加入不完整的移除入口。
