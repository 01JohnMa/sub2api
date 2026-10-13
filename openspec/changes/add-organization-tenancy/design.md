## Context

当前系统已经有用户、API Key、Group 路由、个人计量和全局统计。新增组织的首个真实需求是“按组织查看已有统计”，而不是创建第二套计费系统。

当前代码中的组织 execution、allocation、ledger、reservation 和 reconciliation 路径属于更大的组织计费方案。本变更不把这些路径当作首版前置条件；它们应在后续独立变更中按真实业务需求启用。

## Goals / Non-Goals

### Goals

- 建立独立的 Organization 身份实体，不复用 Group 或上游凭据中的 `organization_id`。
- 复用现有单组织 User 归属和组织角色模型。
- 在请求接受时从服务端状态解析可信组织上下文。
- 让成功 usage 和可归属错误记录保存请求发生时的组织维度。
- 在现有统计查询上增加组织过滤，并保持个人统计行为不变。

### Non-Goals

- 组织资金、allocation、reservation、ledger、结算和 reconciliation。
- 组织与个人余额/订阅的扣款来源切换。
- 为组织请求创建新的执行状态机、provider evidence 或 outbox 事实源。
- 通过当前 User/API Key 归属反推历史 telemetry。
- 组织额度管理、组织账本、归档资金冻结和组织独立 API Key quota。

## Decisions

### 1. Organization 是身份维度，不是资金维度

Organization 只保存 `id`、`name`、规范化唯一 `code`、可选描述、状态和时间戳。首版支持 `active` 与 `disabled`；disabled 组织不能进入组织管理员读接口，重新启用不改变历史 telemetry。暂不引入需要资金结清前置条件的 archived 状态。

Group 继续负责路由、订阅和倍率；上游凭据中的 `organization_id` 继续是 provider 元数据。二者都不能作为平台组织实体。

### 2. 复用现有单组织成员模型

User 保持最多一个当前 Organization 和一个组织角色：`organization_admin` 或 `organization_member`。加入、移除和角色变更复用现有 User 锁与授权检查。首版不引入多组织 membership 表、邀请流或组织间切换。

API Key 仍是个人 Key，不新增组织资金或组织 quota 合同。请求时通过当前 Key 的 owner User 解析组织；成员变更提交后，新请求使用新归属，已完成写入的 telemetry 不被重分类。

### 3. 请求组织归属只来自服务端

请求管线在已有 API Key/User 校验之后读取当前组织状态，并把可空的 `organization_id` 放入服务端上下文。客户端 header、query、body 或前端路由参数中的组织值不参与授权或统计归属。

- 无组织 User：继续走原有个人路径。
- active 组织 User：继续走原有路由、上游和个人计量路径，同时把组织上下文传给 telemetry writer。
- disabled 组织 User：组织范围接口拒绝；是否允许既有个人请求由现有产品状态合同决定，但不得伪造新的组织归属。
- 组织解析失败：不把请求标记为组织请求，也不把客户端值写入 telemetry。

### 4. 复用现有执行和 telemetry writer

不创建组织专用 execution service。现有 handler、Gateway service、provider adapter、重试和 usage/error writer 保持所有执行语义。只扩展已有 telemetry 输入与数据库列：成功 usage 和可归属 ops error 在写入时带上可信组织 ID；无法建立可信组织上下文的认证失败保持 NULL。

统计使用已存在的记录和时间口径。若现有查询分别统计成功 usage 与错误日志，组织查询只在各自查询中增加组织谓词，并沿用现有去重/错误率口径；不引入 final evidence 来修复本变更之外的计费一致性问题。

### 5. 历史归属是写入时快照

组织成员被移除或用户后来加入其他组织时，已有 usage/error 行的 `organization_id` 不变。组织报表只读取写入时快照，不能通过当前 User/API Key 归属回溯历史。

现有无组织数据保持 NULL。迁移不得从 Group、订阅、payment 或 provider OAuth 字段推断组织。

### 6. 组织范围读接口最小化

组织管理员接口只提供当前组织的基本信息、成员摘要和统计概览；所有 repository 查询必须带服务端解析出的组织谓词。目标对象属于其他组织时返回统一的 404，不接受客户端 organization ID。首版不暴露额度、账本、provider evidence、请求正文、错误详情或完整 API Key。

## Minimal Flow

```text
API Key/User authentication
        ↓
trusted current organization lookup
        ↓
existing request / routing / billing pipeline
        ↓
existing usage + error writers (+ nullable organization_id)
        ↓
existing reports (+ organization predicate)
```

## Risks / Trade-offs

- 只保存组织统计维度，不能回答组织剩余额度或组织消费结算问题；这些问题必须另立资金合同。
- 复用现有 usage/error 投影意味着首版沿用其已有统计口径，不借此变更全局成功率、费用或重试语义。
- 每请求读取组织状态会增加一次轻量查询；缓存只能优化性能，不能代替当前归属授权。
- 成员变更与请求并发时，以请求接受时捕获的组织快照写 telemetry；新请求看到提交后的新状态。

## Migration Plan

1. 增加可空组织字段与索引，保留所有现有行 NULL。
2. 复用现有 User/API Key 鉴权链路，写入可信组织上下文。
3. 扩展现有 usage/error writer 和查询谓词。
4. 增加组织只读概览与两组织隔离测试。
5. 在没有组织资金合同的前提下启用组织统计；任何 allocation/reservation/ledger 代码不纳入本次上线门禁。

## Open Questions

无阻塞问题。若未来需要组织独立扣费，应创建新的 OpenSpec change，明确资金来源、reservation、settlement、历史证据和回滚条件，不在本变更中隐式扩展。
