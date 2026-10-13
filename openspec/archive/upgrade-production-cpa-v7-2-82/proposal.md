## Why

生产 CPA 当前运行 CLIProxyAPI `v7.2.61`，已落后于官方 `v7.2.82`，中间版本包含 Responses、自定义工具、OAuth、WebSocket 和模型目录刷新等与现网主流请求相关的修复。需要通过可验证、可回滚的受控升级降低兼容性风险，同时避免把刚发布版本或无关配置改动直接带入生产。

## What Changes

- 在发布门禁通过后，将生产 `cpa.service` 使用的 CLIProxyAPI 二进制从 `v7.2.61` 升级到固定的官方 `v7.2.82` Linux AMD64 资产。
- 升级前固定版本、资产 URL 和 SHA-256，建立运行态基线并完成可执行的二进制回滚备份。
- 采用短暂停机、同文件系统原子替换和自动失败回滚，避免两个实例共享 OAuth 目录并发运行。
- 用脱敏的真实请求覆盖直连 CPA、Sub2API 转发、Responses、Chat Completions、自定义工具以及生产正在使用的模型。
- 建立即时、15 分钟和 24 小时观察窗口，并按服务健康、错误率、延迟、认证池和工具调用稳定性判定成功或回滚。
- 不调整已过时的 `gpt-5.4`，不修改 Sub2API、Nginx、数据库、账号池策略、模型映射、systemd 参数或日志策略。

## Capabilities

### New Capabilities

- `production-cpa-upgrade`: 规定生产 CPA 二进制升级的发布门禁、备份、切换、真实案例验收、监控和回滚合同。

### Modified Capabilities

无。

## Impact

- 受影响系统：生产 CPA 主机上的 `/opt/cpa/bin/cli-proxy-api` 与 `cpa.service`。
- 预期影响：切换窗口内 CPA 及经 Sub2API 转发的请求会有一次不超过 60 秒的短暂不可用。
- API 合同：不主动改变 OpenAI 兼容 API、路由、模型别名或认证合同；升级后的行为差异仅来自上游 CLIProxyAPI 版本。
- 生产数据与凭据：不迁移数据库，不回显密钥、Token、Cookie 或完整请求体；OAuth 目录快照仅用于灾难恢复并保持 root-only。
- 回滚基线：保留当前 `v7.2.61` 二进制作为首选回滚版本；稳定观察完成前不删除已有备份。
