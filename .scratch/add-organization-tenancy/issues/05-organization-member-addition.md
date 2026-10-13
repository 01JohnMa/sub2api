# 05 — 组织管理员直接加入普通成员

**What to build:** 让 active 组织管理员通过准确邮箱或用户 ID 直接加入已注册的无组织用户，并清晰告知加入后现有 Key 会立即成为零额度组织 Key。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 03 — 当前组织身份与隔离入口

**Status:** resolved

- [x] 组织管理员只能按准确邮箱或准确用户 ID 加入本组织普通成员，不提供全局搜索、邀请、邮件或待接受状态。
- [x] 新成员只能获得 `organization_member`，组织管理员不能任命、降级或移除任何组织管理员。
- [x] 不存在、已删除或已属于组织的目标返回相同状态、错误码和响应形状，不泄露账号或其他组织信息。
- [x] 加入事务创建零额度 allocation，并原子转换目标用户所有现有 Key 的组织归属；新建 Key 继承同一组织。
- [x] 前端确认明确警告：现有 Key 会立即转为组织 Key，未单独分配额度前计费请求不可用。
- [x] 成员列表和加入结果只返回本组织允许的成员字段与零额度摘要，不包含个人余额或全局配置。

## Comments

- 组织 Key 在通用、Gemini 和 Antigravity 网关均权威重载并 fail-closed；完整组织计费启用前不会回退到个人余额或订阅。
