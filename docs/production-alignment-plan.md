# main ↔ ccs-v0.2.18 生产对齐计划

> 状态：计划稿，待审批后执行。高风险合并工程，执行前需按本计划逐阶段验收。

## 1. 背景

| 线 | 内容 | 状态 |
|---|---|---|
| `main` | v118 品牌线 + 组织租约全量实现（资金/计费/治理） | 52/52 后端包、2,758/2,758 前端测试全绿 |
| `ccs-v0.2.18` | 线上运行线（上游 v0.2.18 rebase + DeepSeek 400 修复），镜像 `sub2api:ccs-v0218-rebase-20261028` | 生产在跑，内容已固化为 `origin/ccs-v0.2.18` |

两线自共同祖先（上游 PR #7726 合并点）分叉：main 侧约 82 个独有提交，
生产线约 383 个独有提交（含上游 v0.2.11→v0.2.18 升级串）。`git merge-tree`
试合并测得 **约 73 个文件真实冲突**，热点集中在：

- `internal/handler/gateway_handler.go`、`openai_gateway_handler.go`
- `internal/server/router.go`、`routes/admin.go`、`routes/gateway.go`
- `internal/handler/admin/account_handler.go`、`channel_handler.go`
- `internal/service/account*.go`、计费与用量写入路径

## 2. 目标与非目标

**目标**：得到一个同时包含「组织租约功能」与「生产线全部修复/上游升级」的
可部署主干，替换两条并行线。

**非目标**：不改任何一方的行为语义；不借合并顺手重构。

## 3. 推荐方案：以生产线为底，分批移植组织功能

不建议直接 `git merge`（73 处冲突一次性解，审查面过大且易静默丢语义——
本次 org 线合并已证明手工解冲突会留隐患）。改为按层移植、每层可独立验收：

### 阶段 0：准备（0.5 天）

1. 以 `origin/ccs-v0.2.18` 建工作分支 `integration/org-onto-ccs-v0218`。
2. 打基线：后端 `go test ./...`、前端四件套全绿记录（作为每阶段回归基准）。
3. 冻结两条源线：期间 main 与 ccs-v0.2.18 不再进新提交（或约定合入窗口）。

### 阶段 1：schema 与 ent 层（1 天）

- 移植组织实体（organization/allocation/execution/evidence/ledger 等 18 张表）
  与 `users`/`api_keys`/`usage_logs` 的 nullable 组织列。
- 验收：`ent generate` 无 diff；对生产库结构做**加性迁移演练**
  （只增列/增表，不改既有列）；现有表回归测试全绿。

### 阶段 2：service 组织域（2 天）

- 移植 `organization_*.go` 全量服务（资金、额度、执行、证据、对账、报表）。
- 冲突点：`usage_log_repo_insert.go`（生产线重构后的插入文件）上的
  `organization_id` 列接线——按 org 线语义重放（列尾追加、scan/批量同步）。
- 验收：`internal/service`、`internal/repository` 组织域测试全绿。

### 阶段 3：网关与计费语义（2-3 天，风险最高）

- 按端点族逐个移植组织计费语义（每族独立提交，便于二分回退）：
  1. Responses HTTP / WebSocket
  2. Chat Completions 兼容层
  3. Messages（Anthropic）
  4. Embeddings / Images
  5. Gemini / Antigravity
  6. 非计费探测（/v1/models、/v1/usage、count_tokens）
- 重点核对 org 线已修复的合并损伤不再复发：
  - usage 校验口径（Responses 严格 / CC 宽松）与 `UsageObserved` 贯通
  - WS pre-dispatch 类型化错误、failover break 不得被 return 截断
  - 组织单次尝试关闭进程内重试、transport 失败 NoHTTPResponse
- 生产线特有修复必须保留：DeepSeek 400 三连修、间歇 400 静默重试、
  CREDIT_LIMIT 解析语义（构建目录 2026-10-28 的两处固化改动已在
  `origin/ccs-v0.2.18` 内）。
- 验收：52/52 包全绿 + 生产 smoke 矩阵（`rollout-runbook.md` §3）。

### 阶段 4：handler/routes/中间件（1-2 天）

- 组织准入链（Google 路径完整链；主路径 fail-closed + reporting 快照）。
- 路由：组织路由、个人路由 `forbidsOrganizationAdmin`、count_tokens 双语义。
- WS 在途预留豁免、审计中间件 nil 守卫。
- 验收：middleware/routes/handler 三包测试全绿。

### 阶段 5：前端（1 天）

- 组织页面、locale 键、路由守卫、auth store getter（对齐 main 上已修复的
  版本，注意 `@/stores/app` 直连 mock、系统默认页大小语义等已定结论）。

### 阶段 6：上线切换（0.5 天 + 观察期）

1. VPS 构建目录拉取对齐分支，构建新镜像（tag 形如 `ccs-org-align-<date>`）。
2. 按 ops 规矩：改前 `.bak`、说清改动、留一行回滚（compose 镜像 tag 回退）。
3. 灰度：先开非计费组织能力（models/usage），个人路径对照生产基线无回归，
   再按 rollout map 开计费端点；生产环境计费闸门默认保持关闭。
4. 观察：`sh/model-status.sh` 渠道探活、`ops_error_logs` 错误率、
   计费流水对账（组织 ledger vs 个人 usage_logs）48 小时。

## 4. 回滚

- 代码：对齐分支不合入 `ccs-v0.2.18`；线上回滚 = compose 镜像 tag 切回
  `ccs-v0218-rebase-20261028`。
- 数据：组织表为加性新增，回滚镜像后留空表无副作用；`organization_id`
  列 Nullable，个人路径忽略。

## 5. 已知风险

| 风险 | 缓解 |
|---|---|
| 两套计费路径叠加（组织预留 vs 个人在途预留）漏豁免点 | 阶段 3 逐端点族移植+独立验收；WS/HTTP 各写显式豁免测试 |
| 生产线 383 提交中的隐性行为在移植中被覆盖 | 以生产线为底（只加不改）；每阶段 diff 审查"只增组织分支" |
| 手工解冲突再次留损伤（本次 org 合并教训） | 禁大爆炸合并；每族独立提交 + 全量测试 + smoke 矩阵 |
| 模块路径差异（MACOS-DO/sub4api） | 移植一律走本仓路径；从上游 cherry-pick 时全局替换（ops 文档既有规矩） |

## 6. 工作量估计

纯移植+验收约 7-9 个工作日（单人），含 48h 观察期。建议阶段 3 拆两人
并行（端点族互相独立）。
