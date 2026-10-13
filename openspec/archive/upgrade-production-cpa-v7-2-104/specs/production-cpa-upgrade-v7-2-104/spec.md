## ADDED Requirements

### Requirement: 升级使用固定且可验证的官方资产
生产 CPA 升级 MUST 使用经批准的固定 CLIProxyAPI 版本、平台资产和 SHA-256，不得在执行时自动改用新的 latest。

#### Scenario: 固定 v7.2.104 默认 Linux AMD64 资产
- **WHEN** 流程准备本次升级候选
- **THEN** 目标 MUST 为 `v7.2.104` 的 `CLIProxyAPI_7.2.104_linux_amd64.tar.gz`
- **AND** 文件 SHA-256 MUST 为 `993babb37b6de831600f0eb31527ca0f938337e1d1f837d5cf846263affa9724`

#### Scenario: 固定目标之后出现新版本
- **WHEN** 官方在准备或等待期间发布 `v7.2.105` 或之后版本
- **THEN** 流程 MUST 只记录新版本
- **AND** 未经重新审计和用户确认不得改变目标

#### Scenario: 发布龄或官方资产门禁失败
- **WHEN** `v7.2.104` 发布未满 24 小时、release 被撤回、资产摘要与 `checksums.txt` 不一致或出现阻断级公开回归
- **THEN** 流程 MUST 停止
- **AND** MUST NOT 创建 production backup/staging 或进入切换阶段

### Requirement: 切换前准备不得改变生产运行态
流程 MUST 将可不停服准备与实际切换分离，并在用户明确确认前保持生产运行 `v7.2.82`。

#### Scenario: 执行切换前任务
- **WHEN** 用户已授权准备但尚未确认切换
- **THEN** 流程 MAY 创建明确版本的 root-only backup/staging、下载候选、离线校验和执行正常基线请求
- **AND** 流程 MUST NOT 停止、重启或替换 `cpa.service` 和正式二进制
- **AND** 流程 MUST NOT 修改 CPA 配置、OAuth 文件、Sub2API、Nginx 或数据库

#### Scenario: 所有准备门禁通过
- **WHEN** release、生产基线、回滚备份、候选和验收矩阵全部验证通过
- **THEN** 流程 MUST 复核生产仍为 active `v7.2.82`
- **AND** 流程 MUST 暂停并等待用户明确确认切换

### Requirement: 当前生产状态和回滚源必须被验证
升级准备 MUST 以真实运行态为基线，并建立可执行的 `v7.2.82` 二进制回滚材料。

#### Scenario: 记录生产基线
- **WHEN** 准备升级
- **THEN** 流程 MUST 验证 service、ExecStart、正式二进制解析路径、版本/哈希/架构、PID/重启数、`8317/8080` 监听、磁盘和 GLIBC
- **AND** 所有证据 MUST 脱敏

#### Scenario: 创建回滚备份
- **WHEN** 准备阶段写入 backup
- **THEN** backup MUST 位于 `/opt/cpa/backups/cpa-v7.2.82-<UTC>/`
- **AND** MUST 包含当前二进制、配置副本、unit/drop-in、EnvironmentFile 元数据和摘要
- **AND** 回滚二进制 MUST 通过版本、哈希、owner/mode 和可执行性验证
- **AND** 旧 staging 中的同哈希文件 MUST NOT 被当作已经完成的正式回滚备份
- **AND** 已存在的 `v7.2.61` 备份 MUST 作为二级灾备保留

### Requirement: 候选必须在不启动生产实例的情况下通过离线校验
候选二进制 MUST 在切换前完成来源、完整性、平台和版本验证，且不得与生产实例并行加载共享 OAuth 目录。

#### Scenario: 下载并校验候选
- **WHEN** 候选下载至 `/opt/cpa/staging/v7.2.104/`
- **THEN** release digest、`checksums.txt` 和下载文件 SHA-256 MUST 一致
- **AND** 解压后二进制 MUST 为与主机匹配的 Linux AMD64 可执行文件
- **AND** 版本首行 MUST 报告 `v7.2.104`

#### Scenario: 候选离线检查完成
- **WHEN** 候选通过版本和平台检查
- **THEN** 流程 MUST NOT 启动第二个使用生产 OAuth 目录的 CPA 实例

### Requirement: 升级验收覆盖生产主链路和版本特有风险
流程 MUST 保存可比较的升级前稳定信号，并在切换后验证 Responses、Chat、WebSocket/compact、工具、模型、OAuth 和 token accounting。

#### Scenario: 保存升级前稳定信号
- **WHEN** 准备阶段建立基线
- **THEN** 流程 MUST 覆盖 CPA 直连、Sub2API 和公网入口
- **AND** MUST 覆盖生产在用 Responses/Chat 模型、自定义工具及真实使用的 WebSocket/compact 路径
- **AND** MUST 保存 input/cached/output/reasoning token、HTTP/流式终止状态和延迟指标
- **AND** Nginx 最终状态基线 MUST 按 endpoint/status 分别保存总请求数、状态计数、比率和样本量

#### Scenario: 区分既有非法 item ID 错误
- **WHEN** 基线出现已知 `input[6].id` 的 `invalid_value` 400
- **THEN** 流程 MUST 将其标记为切换前既有客户端合同问题
- **AND** MUST NOT 宣称 `v7.2.104` 的超长 ID 修复能够修复该长度 45 的非法 message ID

#### Scenario: 检查版本特有行为
- **WHEN** 切换后执行立即验收
- **THEN** 流程 MUST 验证 token accounting 没有重复、负数或字段丢失
- **AND** MUST 验证工具名、namespace（当前基线预期为空，意外出现即阻断）、`call_id` 和 tool-result continuation 可解析
- **AND** MUST 验证生产在用模型、OAuth 文件数量和可调度账号均未减少
- **AND** MUST 验证 plugins 仍为 disabled 且未自动加载
- **AND** MUST 使用真实或等价的 Codex session signal 验证 `session-affinity=false` 的现有轮询语义没有改变
- **AND** 新增日志 metadata MUST NOT 暴露 Authorization、Cookie、API key、完整请求体或原始敏感会话标识

#### Scenario: 候选版本用最终 Trace 覆盖证明三个账号可调度
- **WHEN** 候选 `v7.2.104` 执行 `live` 或 `final_live`
- **THEN** 流程 MUST 从管理快照读取恰好三个唯一 `.auth_index`，每个值 MUST 匹配 `^[0-9a-f]{16}$`
- **AND** `/auth-files/models?name=...` MUST 确认 selected model 在三个预期凭据的模型目录中均存在
- **AND** 流程 MUST 使用非流 selected-model 短探针，在最多 12 次且绝对 30 秒内获得三个预期索引各至少一次最终成功响应
- **AND** 最终响应的 `X-CPA-TRACE-ID` MUST 只从最终 HTTP header block 读取，且 MUST 恰好一个值并匹配 `^[0-9]{14}-[0-9a-f]{16}-[0-9a-f]{8}$`
- **AND** 缺失、重复、未知、格式错误、超预算或未覆盖 3/3 MUST 阻断并触发自动回滚，且不得转化为已有 transport retry
- **AND** `success`/`failed` 增量 MUST 仅校验为三个非负整数且无计数倒退，不得以数值大小单独判定失败或重试
- **AND** `v7.2.82` legacy 回滚验收 MUST NOT 要求 `X-CPA-TRACE-ID`
- **AND** Sub2API localhost 与公网 Responses 成功响应中该头计数 MUST 为零；Trace 原始值 MUST NOT 写入日志或长期证据

#### Scenario: 验证原生会话信号但保持当前非粘性合同
- **WHEN** 当前配置为 `session-affinity=false`
- **THEN** 流程 MUST 保存升级前正常 Codex session signal 的多请求与工具续接基线
- **AND** 切换后 MUST 使用同一类会话信号完成对应请求
- **AND** MUST 确认未启用隐式粘性、未改变 round-robin 合同且没有新增 invalid credential/no available account

#### Scenario: 验证日志 metadata 传播
- **WHEN** 切换后处理正常 Codex/Responses 请求
- **THEN** 流程 MUST 脱敏抽样新增日志字段并验证敏感 header、API key、完整 body 和原始敏感会话标识未泄露
- **AND** 相同请求量口径的日志字节/请求增幅 MUST 不超过 20%
- **AND** 按观察速率推算 MUST 不会触及当前 200MB 日志上限

#### Scenario: 条件覆盖 Codex Live 和图像 WebSocket
- **WHEN** 最近 24 小时生产基线发现 Codex Live、sideband、WebRTC 或图像重 payload 路径
- **THEN** 对应路径 MUST 加入升级前后强制验收
- **AND** 图像 WebSocket 出现 `1009` 时 MUST NOT 形成账号轮换风暴
- **WHEN** 基线未发现这些路径
- **THEN** 流程 MUST 记录未发现真实使用且不得为测试擅自开启生产功能

#### Scenario: 覆盖 SSE 非 SSE 工具和长历史 replay
- **WHEN** 执行升级前后真实案例矩阵
- **THEN** 流程 MUST 各执行至少一次 SSE 和非 SSE 自定义工具调用及 tool-result continuation
- **AND** MUST 执行长历史 Responses 请求并比较稳定完成、P95 和 TTFT
- **AND** 每个成功的 SSE 案例 MUST 只有一个终态且终态位于事件流最后；一次 `dual_terminal`、非法终态数量或非法终态顺序 MAY 消耗共享的唯一完整重试，第二次仍异常 MUST 阻断切换

#### Scenario: 区分 HTTP SSE 与 Responses WebSocket v2 续接合同
- **WHEN** 验证工具调用和上下文续接
- **THEN** CPA 直连 MUST 验证普通 `previous_response_id` continuation
- **AND** Sub2API HTTP 的 SSE 与非 SSE 工具续接 MUST 使用显式 `function_call` + `function_call_output` replay
- **AND** 流程 MUST 将 Sub2API HTTP tool-result `previous_response_id` 的既有 400 视为路径合同而非 `v7.2.104` 回归
- **AND** HTTP `stream:true` MUST NOT 被当作 Responses WebSocket v2 验收
- **AND** 只有近 24 小时存在真实 Responses WebSocket v2 使用时，流程 MUST 使用真实 WebSocket 协议加入对应升级前后门禁

### Requirement: 数据同步存储兼容性必须在写入前确认
流程 MUST 在创建候选或切换前确认生产没有启用已知存在数据保护风险或配置重命名影响的存储模式。

#### Scenario: 检查 GitTokenStore 与插件同步配置
- **WHEN** 流程读取生产配置结构和启动参数
- **THEN** MUST 确认未启用 GitTokenStore
- **AND** MUST 确认不存在需要迁移的 `plugins.sync-revision` 旧键
- **AND** 如果任一条件不满足 MUST 停止并单独评审数据恢复和配置迁移

### Requirement: CPA 升级窗口必须与其他生产发布互斥
CPA 准备、切换和观察的指标比较 MUST 在 Sub2API、Nginx 和相关配置保持不变的条件下进行。

#### Scenario: 建立升级基线
- **WHEN** 流程保存切换前基线
- **THEN** 流程 MUST 记录 Sub2API 当前运行镜像 digest 或等价不可变标识
- **AND** MUST 确认 CPA 切换与短时观察期间没有计划中的 Sub2API/Nginx 重建或配置发布

#### Scenario: 观察期间出现其他发布
- **WHEN** Sub2API、Nginx 或其配置在 CPA 基线、切换或短时观察期间发生变化
- **THEN** 当前对比窗口 MUST 失效
- **AND** 流程 MUST 重新建立基线或停止并请求确认

### Requirement: 用户确认后的切换必须原子且可自动回滚
只有用户明确确认后，流程才可停服和替换二进制；进程或关键业务门禁失败时 MUST 自动恢复 `v7.2.82`。

#### Scenario: 执行原子切换
- **WHEN** 用户明确确认切换且低流量/OAuth 门禁通过
- **AND** controller 已将通过 manifest/self-hash 的脚本冻结为只读本地快照，并从该快照执行基线、切换、验收和短观察
- **THEN** 流程 MUST 停止旧服务并确认旧 PID 退出及 `8317` 无监听
- **AND** MUST 在停服后创建 root-only OAuth 一致性快照
- **AND** OAuth 数据 MUST 复制到始终保持 `root:root 0700` 的快照外层目录内；复制完成后 MUST 递归强制并验证所有目录为 `root:root 0700`、所有普通文件为 `root:root 0600`
- **AND** OAuth 快照 MUST 拒绝 symlink 和特殊文件，且 MUST 递归覆盖全部普通数据文件生成 manifest 并立即静默执行完整性校验
- **AND** MUST 使用同文件系统临时文件、`root:root`、`0755` 和原子重命名替换正式二进制
- **AND** MUST 在远端 switch/rollback 入口持有同一 root-only `flock`，防止 SSH 断开后的远端回滚与独立回滚并发 stop/install/start
- **AND** MUST 使用唯一 run ID 在 root-only 协调目录中持有 owner token，覆盖切换、立即验收和 15/30 分钟短观察；不同 owner 的控制器 MUST NOT 切换、释放或回滚当前窗口
- **AND** 创建 owner 前 MUST 在共享锁内确认正式服务仍为精确 `v7.2.82`；当前已是其他版本时 MUST 拒绝认领且不得通过退出清理触发降级
- **AND** owner 认领结果不确定时，匹配相同 run ID 才可保守回滚；认领未确认且 owner 不存在时 MUST 不执行回滚
- **AND** 已确认 owner 或开始 release 后 owner 意外缺失 MUST 视为歧义并保守回滚；回滚发现不同 owner 时 MUST 只返回拒绝状态且不得改变生产运行态
- **AND** MUST 在 30 秒内验证 active、监听和版本 `v7.2.104`
- **AND** 停止、启动及恢复命令 MUST 受绝对 deadline 约束，不能只在结束后检查耗时
- **AND** systemd start/stop MUST 使用 `--no-block` 后按状态轮询；因生产 unit 默认 90 秒超时超过本次合同，停止 5 秒仍未完成时 MUST 按经审查策略向 `control-group` 发送 `SIGKILL` 并再次验证 PID/监听退出
- **AND** 进程切换失败时从首次停止开始到恢复 `v7.2.82` 的总不可用时间 MUST NOT 超过 60 秒
- **AND** MUST 验证 systemd `ExecStart`、unit/drop-in 摘要和 `/proc/$MainPID/exe` 与正式路径一致
- **AND** `EXIT`、`INT`、`TERM` 或 `HUP` 在立即业务门禁和 15/30 分钟短观察完成前 MUST 保持自动回滚控制器为 armed

#### Scenario: 启动或关键业务门禁失败
- **WHEN** 服务、监听、版本、生产模型、账号池、token accounting 或关键真实案例任一不符合合同
- **THEN** 流程 MUST 停止候选并从固定 backup 原子恢复 `v7.2.82`
- **AND** 回滚命令 MUST 在返回成功前验证 active、`8317`、固定摘要和精确 `v7.2.82` 版本行
- **AND** MUST 重新完成生产链路验收
- **AND** 正常回滚 MUST NOT 覆盖当前 OAuth 目录

### Requirement: 升级成功必须经过立即验收和短时观察
切换后的成功判定 MUST 依赖立即和至少 15 分钟的短观察，而不是只依赖进程启动；样本少于 20 条时 MUST 延长至 30 分钟。

#### Scenario: 立即和短时观察
- **WHEN** 切换后进入观察期
- **THEN** 关键真实案例 MUST 连续通过
- **AND** `live`/`final_live` 的匿名全局失败增量 MUST 仅作为观测值，并且必须是三个非负整数且没有计数倒退；任何数值大小 MUST NOT 单独导致失败或触发重试
- **AND** `status_message` 中的硬认证匹配（`invalid_grant`、invalid credentials、authentication error、unauthorized、no-available-account、refresh failure、401/403）在前后快照 MUST 为零；状态/身份/模型/请求等其他门禁失败时 MUST 立即阻断且不得第三次重试
- **AND** MUST 使用同口径窗口按 endpoint/status 分别比较总请求数、状态计数、比率和样本量
- **AND** 新增或未分类 5xx MUST 不超过 1%
- **AND** 既有 5xx signature 的比率 MUST 不超过“刷新后基线比率加 2 个百分点”和“刷新后基线比率的 1.5 倍”两者中的较大值，且上限为 100%
- **AND** 上述既有 signature 容差 MUST NOT 覆盖关键案例连续两次失败的阻断条件
- **AND** P95 总时长和 P95 TTFT 相对同口径基线增幅 MUST 不超过 20% 或有可验证的流量结构解释
- **AND** 样本少于 20 条时 MUST 延长观察至 30 分钟

#### Scenario: 短观察完成并保留回滚能力
- **WHEN** 立即验收、适用的 15/30 分钟短观察、最终 `live`、最终 process 和最终 log 门禁全部通过
- **THEN** 流程 MUST 先释放匹配的 owner token，并确认 release 成功
- **AND** 只有 release 确认成功后，流程 MUST 标记本次升级成功并解除自动回滚 controller
- **AND** 流程 MUST 继续保留 `v7.2.82` 回滚备份
- **AND** 回滚备份的整理或删除 MUST 等待单独确认，不得随短观察成功自动执行

### Requirement: 升级证据不得泄露敏感信息
所有准备、切换、验收和回滚证据 MUST 脱敏，不得输出密钥、Token、Cookie、认证文件内容、私有请求正文或生产个人数据。

#### Scenario: 采集生产证据
- **WHEN** 流程读取配置、日志、OAuth 状态或真实请求结果
- **THEN** 证据 MUST 只保留版本、摘要、计数、状态码、稳定字段和脱敏指标
- **AND** 完整错误请求体和认证文件内容 MUST NOT 出现在命令输出、OpenSpec 或最终报告中
