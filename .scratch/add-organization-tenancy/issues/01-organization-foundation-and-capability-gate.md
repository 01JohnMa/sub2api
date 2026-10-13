# 01 — 平台组织基础与 capability 门禁

**What to build:** 建立平台组织的最小可用基础，让平台管理员能够创建、查看和编辑组织，同时以明确的 capability matrix 保证所有组织计费 endpoint 在完成适配前默认关闭。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** None — can start immediately

**Status:** resolved

> 实施边界：本票据建立组织 spending 的静态 capability matrix、配置总闸和临时全局 fail-closed 合同。当前 gateway 会权威重载 Key/User 归属并拒绝全部组织 Key；按 endpoint shape 的额度预留与结算仍由 Ticket 08 验收。

- [x] 平台管理员可以创建、列表、查看和编辑组织名称、规范化唯一 code 与描述，新组织默认为 active。
- [x] archived code 永不复用，正常 API 不提供组织硬删除，现有用户、API Key 和历史遥测升级后仍保持无组织。
- [x] 新注册用户默认保持 `organization_id = null`、无组织角色和现有个人计费/Key 语义，只有显式加入事务才会进入组织。
- [x] Gateway capability matrix 明确列出 endpoint family、request shape、成本上界支持状态和 rollout 开关，组织 spending 默认全部关闭。
- [x] 在 Ticket 08 完成前，任何组织归属或归属不一致的 Key 都在 gateway 鉴权阶段返回稳定错误，包括 SimpleMode，不回退个人余额或订阅。
- [x] 平台管理 UI 能完成组织创建、查看和编辑，并且不引入 Group、上游 OAuth organization_id 或 CPA 轮询语义变化。

## Comments

- 当前只实现全局组织 Key fail-closed；真实组织状态/allocation 解析、endpoint ceiling、预留与结算继续由 Ticket 08 验收。
