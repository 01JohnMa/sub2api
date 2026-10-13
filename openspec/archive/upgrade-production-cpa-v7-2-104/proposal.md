## Why

生产 CPA 当前运行 CLIProxyAPI `v7.2.82`，本次批准目标固定为 `v7.2.104`；官方 latest 已推进到 `v7.2.113`，但不改变冻结目标。中间 22 个补丁版本包含 Responses/WebSocket 连续性、工具调用与 item ID 处理、Codex Live、token accounting、OAuth/账号调度、插件宿主稳定性和可观测性修复，需要通过固定版本、可回滚且由用户最终确认切换的流程降低现网兼容风险。

## What Changes

- 将本次生产升级目标固定为官方 CLIProxyAPI `v7.2.104` Linux AMD64 默认资产及其官方 SHA-256，不在执行中自动跟随后续 latest。
- 在不停止或重启生产 CPA 的前提下，完成 release 门禁、生产基线、回滚备份、候选下载与离线验证、真实链路基线和切换脚本/命令审查。
- 所有切换前门禁通过后保持 `v7.2.82` 继续运行，等待用户再次明确确认；未经确认不得停服、替换二进制或重启服务。
- 用户确认后，仅替换 `cpa.service` 使用的 CPA 二进制，并对直连 CPA、Sub2API、公网入口、Responses、Chat Completions、WebSocket/compact、自定义工具和生产模型执行验收。
- 建立即时和至少 15 分钟的短观察窗口；样本少于 20 条时延长至 30 分钟。关键合同、账号池、错误率或延迟回归时自动恢复固定的 `v7.2.82` 回滚二进制。
- 对候选 `v7.2.104` 使用官方 `X-CPA-TRACE-ID` 做 candidate-only 的三账号最终成功覆盖门禁；最多 12 个非流短探针、绝对 30 秒，Trace 只在 root-only 临时目录内解析。Sub2API localhost 与公网 Responses 路径不得透传该头；`v7.2.82` 回滚验收继续使用兼容路径。
- 不启用插件，不修改 `session-affinity`、账号权重、模型映射、CPA 配置、systemd unit、Sub2API、Nginx、数据库或日志策略。

## Capabilities

### New Capabilities

- `production-cpa-upgrade-v7-2-104`: 规定从生产 CPA `v7.2.82` 升级至固定 `v7.2.104` 的发布门禁、切换前准备、显式用户确认点、切换验收、观察和回滚合同。

### Modified Capabilities

无。

## Impact

- 受影响系统：生产主机上的 CPA staging/backup 目录，以及用户确认后 `/opt/cpa/bin/cli-proxy-api` 和 `cpa.service`。
- 切换前影响：只创建 root-only 回滚材料和候选文件，不改变运行中进程、端口、配置、OAuth 文件或请求路由。
- 用户确认后的预期影响：切换窗口内 CPA 及经 Sub2API 转发的请求会有一次不超过 60 秒的短暂不可用。
- API 合同：不主动修改 OpenAI 兼容 API、模型别名、认证或调度合同；行为差异仅来自官方 CLIProxyAPI 二进制版本。
- 候选版本的 CPA 直连响应可能带官方 `X-CPA-TRACE-ID` 稳定关联头，仅用于升级期间的匿名账号归因；Sub2API localhost 与公网 Responses 过滤该头，不将其作为公网 API 合同。
- 生产数据与凭据：不迁移数据库，不回显密钥、Token、Cookie、认证文件名或完整请求体；OAuth 快照只能在停服后的切换阶段创建，并仅用于灾难恢复。
- 回滚基线：切换前必须固定并验证当前 `v7.2.82` 二进制备份；短观察成功后仍保留，只有单独确认后才可整理或删除。
