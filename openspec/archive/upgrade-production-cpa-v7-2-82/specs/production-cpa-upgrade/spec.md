## ADDED Requirements

### Requirement: 固定且可信的升级目标
生产 CPA 升级 MUST 使用经批准的固定 CLIProxyAPI 版本、平台资产和 SHA-256，不得在执行时自动改用新的 latest 版本。

#### Scenario: 固定资产通过发布门禁
- **WHEN** 官方 `v7.2.82` 已发布满 24 小时、Linux AMD64 资产存在且摘要与批准值一致
- **THEN** 升级流程可以继续进行生产基线与备份

#### Scenario: 官方 latest 在执行前变化
- **WHEN** 执行时发现比 `v7.2.82` 更新的版本
- **THEN** 流程继续以已批准的 `v7.2.82` 为目标，且不得自动改变版本

#### Scenario: 发布门禁失败
- **WHEN** 资产摘要不一致、release 被撤回或发现高严重度阻断回归
- **THEN** 升级 MUST 停止且不得改动生产二进制

### Requirement: 升级前建立可比较的生产基线
升级流程 MUST 在切换前保存脱敏的服务健康、流量质量、模型目录和真实请求稳定信号基线。

#### Scenario: 记录运行态基线
- **WHEN** 准备进入生产切换窗口
- **THEN** 基线包含当前版本、systemd 状态、启动命令、`8317/8080` 监听、磁盘空间、请求量、5xx、认证错误、P95 总时长和 P95 TTFT

#### Scenario: 记录生产在用能力
- **WHEN** 汇总最近 24 小时的生产请求
- **THEN** 基线包含在用模型以及 `ultra`、WebSocket/compact、自定义工具和 OAuth 流程的实际使用情况

#### Scenario: 排除过时模型噪声
- **WHEN** 计算本次升级的成功指标
- **THEN** 已明确过时且不在范围内的 `gpt-5.4` MUST 不作为升级失败判据

### Requirement: 提供经过验证的回滚材料
升级流程 MUST 在替换生产二进制前创建并验证当前 `v7.2.61` 的回滚备份。

#### Scenario: 备份验证成功
- **WHEN** 已解析实际 `ExecStart` 路径，并保存当前二进制、配置副本、unit/drop-in、EnvironmentFile 元数据和停服后创建的 root-only 认证快照
- **THEN** 流程验证二进制版本、SHA-256、所有者和执行权限，并固定可执行的回滚源与目标

#### Scenario: 创建一致的认证快照
- **WHEN** 旧 CPA 服务已 inactive、旧 PID 已退出且 `8317` 已无监听
- **THEN** 流程从实际配置解析认证目录，保存 owner/mode/ACL/xattr 和不回显认证内容的校验清单

#### Scenario: 备份不可用
- **WHEN** 备份版本、哈希、权限或可执行性校验失败
- **THEN** 升级 MUST 在停服前停止

### Requirement: 候选二进制必须离线验证
目标二进制 MUST 在不启动共享认证目录的第二个 CPA 实例的前提下完成平台、摘要、权限和版本检查。

#### Scenario: 候选符合目标平台
- **WHEN** 候选资产已下载并解压至 staging 目录
- **THEN** 其架构为 Linux AMD64、摘要等于固定值、版本输出为 `v7.2.82` 且正式安装权限可设为 `root:root`、`0755`

#### Scenario: 版本命令返回非零状态
- **WHEN** `--version` 已输出正确首行但程序以已知状态码 2 退出
- **THEN** 流程以版本字符串作为校验结果且不得仅因该状态码拒绝候选

#### Scenario: 候选需要共享 OAuth 目录运行
- **WHEN** 离线检查无法在不运行第二实例的情况下完成
- **THEN** 流程 MUST 放弃并行 canary，改在受控切换后验证

### Requirement: 切换必须最小化且有时间边界
生产切换 SHALL 只替换 CPA 二进制，并 MUST 在同文件系统内原子安装，保持既有配置和服务合同不变。

#### Scenario: 正常切换
- **WHEN** 所有前置门禁通过且进入低流量窗口
- **THEN** 流程停止 `cpa.service`、原子替换二进制、设置 `root:root` 与 `0755` 并重新启动服务

#### Scenario: 旧进程未完全退出
- **WHEN** 服务停止后仍为 active、旧主 PID 仍存在或 `127.0.0.1:8317` 仍在监听
- **THEN** 流程 MUST 停止且不得替换正式二进制

#### Scenario: 启动健康检查失败
- **WHEN** 启动后 30 秒内服务未 active、`127.0.0.1:8317` 未监听或版本不是 `v7.2.82`
- **THEN** 流程 MUST 立即恢复 `v7.2.61` 二进制

#### Scenario: 切换范围发生扩张
- **WHEN** 执行需要修改 Sub2API、Nginx、数据库、配置、systemd 参数、账号池或模型映射
- **THEN** 本次升级 MUST 停止并将额外变更单独评审

### Requirement: 使用三层真实链路验收
升级后 MUST 验证进程层、CPA 直连层和 Sub2API 生产链路层，并以稳定信号判断结果。

#### Scenario: 进程层通过
- **WHEN** 新二进制启动
- **THEN** systemd 持续 active、版本为 `v7.2.82`、`8317` 正常监听且启动日志无 panic 或持续重启

#### Scenario: 模型目录通过
- **WHEN** 认证访问升级后的 CPA `/v1/models`
- **THEN** 升级前生产在用的模型全部存在

#### Scenario: 主流请求通过
- **WHEN** 回放 `gpt-5.6-sol` Responses、`codex-auto-review` Responses、`gpt-5.5` Responses、`gpt-5.5` Chat Completions、公网入口 Responses 和自定义工具代表案例
- **THEN** HTTP/流式结束状态、响应类型、模型、文本非空、工具名、`call_id` 与可解析参数符合升级前合同

#### Scenario: 自定义工具多轮合同通过
- **WHEN** 代表案例产生自定义工具调用
- **THEN** 流程完成一次 tool-result continuation，且工具名、`call_id` 和参数保持可解析

#### Scenario: 实际使用的附加能力通过
- **WHEN** 基线确认生产正在使用 `ultra`、WebSocket/compact 或其他附加路径
- **THEN** 对应代表案例 MUST 在升级后通过才能进入观察期

#### Scenario: 关键案例连续失败
- **WHEN** 任一关键案例在一次重试后仍失败
- **THEN** 流程 MUST 触发二进制回滚

### Requirement: 升级结果必须经过分阶段观察
升级 MUST 经过立即、15 分钟和 24 小时观察，并使用与基线相同的统计口径。

#### Scenario: 15 分钟观察通过
- **WHEN** 切换后连续观察至少 15 分钟且非 `gpt-5.4` 样本不少于 20 条
- **THEN** 服务无异常重启、5xx 不超过 1%、无持续认证池错误，且同一数据源下 P95 总时长和 P95 TTFT 相对基线增幅均不超过 20%

#### Scenario: 观察样本不足
- **WHEN** 15 分钟内非 `gpt-5.4` 生产样本少于 20 条
- **THEN** 流程 SHALL 延长观察到 30 分钟且不得以小样本判定通过

#### Scenario: 24 小时观察通过
- **WHEN** 切换后 24 小时内模型目录、认证刷新、错误率、延迟、工具调用和日志磁盘增量均无阻断回归
- **THEN** 升级可以标记为成功并进入旧备份整理

#### Scenario: 观察期出现阻断回归
- **WHEN** 出现生产在用模型缺失、持续 OAuth/账号池错误、工具合同回归、5xx 超阈值或无法解释的延迟超阈值
- **THEN** 流程 MUST 回滚至 `v7.2.61`

### Requirement: 回滚不得覆盖正常推进的认证状态
正常回滚 MUST 只恢复已验证的 `v7.2.61` 二进制，除非有单独证据和评审证明配置或认证数据已经损坏。

#### Scenario: 二进制回滚
- **WHEN** 任一强制回滚条件触发
- **THEN** 流程停止服务、原子恢复 `v7.2.61`、启动服务并重新执行三层验收

#### Scenario: OAuth Token 在升级窗口内刷新
- **WHEN** 回滚时认证状态可能已经推进
- **THEN** 流程 MUST 保留当前认证目录且不得默认恢复旧快照

#### Scenario: 需要恢复认证快照
- **WHEN** 有证据确认认证目录损坏
- **THEN** 恢复操作 MUST 经过单独评审并在执行前获得确认

### Requirement: 升级证据必须脱敏且可追溯
升级记录 MUST 包含版本、时间、摘要、健康状态、指标和稳定信号结果，同时不得包含密钥、Token、Cookie、完整敏感请求体或生产用户数据。

#### Scenario: 保存升级证据
- **WHEN** 完成升级、观察或回滚阶段
- **THEN** 记录使用 UTC 时间线和脱敏结果说明每个门禁的通过、失败或未验证状态

#### Scenario: 清理旧备份
- **WHEN** `v7.2.82` 已稳定运行 24 小时且回滚材料仍有效
- **THEN** 保留 `v7.2.61` 作为唯一上一版本备份，并仅在确认后删除更旧的 `v7.2.42` 备份
