# Change: Add organization reporting dimension

## Why

当前系统的用户、API Key、usage 和错误统计没有组织维度。我们需要增加一个最小的 Organization 归属，使平台能够按组织查看已有请求、Token、模型和错误统计。

本变更不把 Organization 变成新的资金来源。首版继续使用现有个人请求、路由和计量逻辑；Organization 只负责身份范围和历史统计归属。

## What Changes

- 新增独立的 `organizations` 身份实体和 active/disabled 状态。
- 在现有 User 身上保留单组织归属与组织角色，复用现有成员加入/移除与 JWT 组织范围校验。
- 每次请求从服务端当前 User/API Key 关系解析可信 `organization_id`，不接受客户端提供的组织值。
- 复用现有上游路由、请求执行、usage/error writer 和报表聚合；成功/错误记录在写入时固化当时的 `organization_id`。
- 组织报表只在现有统计查询上增加组织谓词，避免另建 execution journal 或组织账务事实源。
- 已有无组织 User、API Key 和历史 telemetry 保持 `organization_id = NULL`，继续按个人逻辑运行。

## Explicit Non-Goals

首版不实现也不要求：

- 组织授信、未分配资金池、成员 allocation、reservation、ledger 或消费结算；
- 组织 Key 独立余额、组织 Key 的个人余额回退规则；
- attempt/provider evidence、final execution evidence、pending reconciliation 或 outbox 账务链；
- 组织归档前结清资金、退款、提现、跨组织转移；
- 为每个 gateway endpoint 建立组织专用 billing adapter；
- 替换现有全局 dashboard 的事实来源或新增组织预聚合缓存。

## Capabilities

### New Capabilities

- `organization-lifecycle`: 组织身份、状态和平台管理员管理边界。
- `organization-membership`: 单组织成员关系与组织角色。
- `organization-resource-attribution`: 请求时可信组织归属和历史 telemetry 归属。
- `organization-scoped-access`: 组织管理员的服务端范围校验和只读统计视图。

### Deferred Capabilities

- `organization-balance-allocation`: 组织资金、成员额度、预留、账本和结算，另立变更。

## Impact

- 数据模型：增加 organizations 及 User/telemetry 的可空组织归属字段；不改变个人余额、订阅或现有 usage cost 语义。
- 后端：复用现有请求管线，仅增加一次服务端组织解析和组织查询谓词。
- 报表：复用现有 usage/error 聚合，按写入时固化的 `organization_id` 过滤；不从当前成员关系反推历史组织。
- 前端：只增加组织摘要、组织范围入口和只读概览；不增加额度、账本或授信页面。
- 验证：覆盖两组织 IDOR、成员变更后的新旧请求归属、旧数据 NULL 兼容、统计过滤和个人路径不回归。

## Rollout

先部署可空字段和兼容代码，所有已有数据保持无组织。创建组织并加入成员后，仅启用组织范围统计；组织不会改变已有请求的计费来源。组织独立资金与结算必须在单独变更中重新定义、验证和发布。
