## Context

生产链路当前为 Cloudflare/Nginx → Sub2API `127.0.0.1:8080` → CPA `127.0.0.1:8317`。CPA 由 `cpa.service` 运行 `/opt/cpa/bin/cli-proxy-api -config /opt/cpa/config.yaml`，当前版本为 CLIProxyAPI `v7.2.82`（提交 `f583414f`）。运行配置包含 3 个 Codex OAuth 凭据、普通 `round-robin`、`session-affinity=false`，且没有启用或安装插件。

目标固定为官方 `v7.2.104` 默认 Linux AMD64 资产 `CLIProxyAPI_7.2.104_linux_amd64.tar.gz`，官方 checksums 中 SHA-256 为 `993babb37b6de831600f0eb31527ca0f938337e1d1f837d5cf846263affa9724`。该 release 于 `2026-07-28 16:35` 左右（Asia/Shanghai）发布，切换前必须再次确认仍是已批准目标且未被撤回。

`v7.2.82` 到 `v7.2.104` 跨越 22 个补丁版本和大量运行时变更。与生产主链路最相关的是 Responses/WebSocket 上下文与工具缓存修复、Codex input item ID 清理、token accounting v2、工具名与 namespace 处理、Codex Live/模型目录、OAuth/凭据调度和日志元数据传播。当前已知的重复 `input[6].id` 400 是长度 45 的客户端 message item ID 合同问题，不属于新版本的“超长 ID”修复，必须作为既有基线单独记录，不能用它误判升级回归或宣称升级会修复。

用户已经授权执行切换前准备，但明确要求在实际切换前等待其回来确认。因此，本变更将“可不停服完成的准备”和“会改变生产运行态的切换”设为两个独立阶段。

## Goals / Non-Goals

**Goals:**

- 固定并验证 `v7.2.104` 官方资产、摘要、平台兼容性和发布门禁。
- 在不影响运行中 CPA 的情况下完成生产基线、可执行的 `v7.2.82` 回滚备份、候选落盘和离线验证。
- 建立覆盖当前生产模型、Responses、Chat Completions、WebSocket/compact、自定义工具、token accounting 和 OAuth 账号池的前后验收矩阵。
- 所有切换前门禁通过后保持生产继续运行 `v7.2.82`，等待用户明确确认。
- 用户确认后将停机控制在 60 秒内，失败时自动恢复 `v7.2.82`。
- 通过立即和至少 15 分钟的短观察证明升级没有引入生产阻断回归；样本少于 20 条时延长至 30 分钟。

**Non-Goals:**

- 不升级、重启或修改 Sub2API、Nginx、数据库及其他服务。
- 不修改 `config.yaml`、systemd unit/drop-in、账号池、权重、`session-affinity`、模型映射、日志策略或插件配置。
- 不启用插件或切换到 `no-plugin` 资产。
- 不在本次升级中修复既有的客户端 `input[].id` 合同错误。
- 不自动跟随 `v7.2.105` 或之后版本；目标变化必须重新审计并由用户确认。

## Decisions

### 1. 固定版本、默认 Linux 资产和官方摘要

本变更固定 `v7.2.104`、`CLIProxyAPI_7.2.104_linux_amd64.tar.gz` 和 SHA-256 `993babb37b6de831600f0eb31527ca0f938337e1d1f837d5cf846263affa9724`。执行时同时校验 release 页面资产 digest、`checksums.txt` 和下载文件摘要。

选择默认资产而不是 `linux_amd64_no-plugin`，是为了保持当前二进制能力边界并避免引入构建形态变化；插件仍由配置保持关闭。候选必须通过 `file`、主机架构、GLIBC 基线和版本首行校验。

### 2. 将准备阶段和切换阶段设置为硬门禁

准备阶段允许创建 root-only backup/staging、下载候选、做离线版本检查和发起正常的脱敏基线请求。准备阶段禁止 `systemctl stop/restart`、覆盖正式二进制、写 OAuth 目录或修改配置。

当准备任务全部完成后，流程必须暂停并记录：目标版本、候选摘要、当前运行版本、回滚源、基线指标和未解决风险。只有用户在后续明确确认“切换”后，才能进入停服与替换步骤。

每次获准执行时，controller 在收集基线前重新检查近 24 小时条件协议使用；发现真实 WebSocket/Live/sideband/WebRTC/图像或视频流量时停止并补充真实协议案例。通过后把已校验脚本目录复制为只读本地快照，后续切换、验收和 15/30 分钟观察只从该快照执行，避免共享工作区在长观察期间发生工件漂移。

### 3. 只替换 CPA 二进制

切换仅触及解析后的 `ExecStart` 正式二进制路径。配置、unit、Sub2API 路由和 OAuth 目录保持原状，以便将行为差异归因于官方版本。

加权轮询、session affinity、插件和新模型映射均另行评审；不与版本升级混合。

### 4. 不运行共享生产 OAuth 目录的并行候选实例

候选只做离线架构和版本检查，不以第二实例加载 `/opt/cpa` 的生产认证目录。两个实例并行刷新同一 OAuth 状态可能造成写入竞争，风险高于受控短暂停机。

### 5. 二进制回滚优先，OAuth 快照只在停服后创建

只读审计确认 `/opt/cpa/backups` 当前没有正式的 `v7.2.82` 回滚条目；旧 staging 中虽有与正式二进制同哈希的文件，但不能替代带完整元数据和验证证据的回滚快照。准备阶段必须备份当前 `v7.2.82` 二进制、配置副本、完整 unit/drop-in、EnvironmentFile 元数据和哈希，并证明回滚二进制可执行。已存在的 `v7.2.61` 备份继续保留为二级灾备，不在本变更中删除。OAuth 一致性快照必须等用户确认、旧服务完全停止后再创建。快照使用始终保持 `root:root 0700` 的外层目录，`cp -a` 只写入其 `auth/` 子目录，使复制失败、symlink/特殊文件拒绝或硬化超时也不会暴露内部宽权限文件。复制后使用非 foreground 进程组 timeout 覆盖全部子进程，显式传播每次 `find` 错误，递归重置并验证目录 `0700`、普通文件 `0600`，递归生成覆盖全部数据文件的 manifest 并立即校验。

正常回滚只恢复固定 `v7.2.82` 二进制；OAuth 快照仅在证据证明认证文件损坏并再次评审后使用，避免覆盖切换期间已推进的 token。

### 6. 使用稳定信号而非文本逐字比较

验收比较 HTTP/流式终止状态、响应类型、模型、文本非空、token usage、cached tokens、工具名、`call_id`、参数可解析、账号池可用性和错误分类。自然语言内容不要求逐字一致，证据中不保存密钥或完整请求体。

### 7. 对版本跨越设置专门门禁

- **Token accounting v2**：升级前后使用同一类 Responses/Chat 请求核对 input、cached、output 和 reasoning token 的结构与 Sub2API 记录；出现负数、重复计量、字段丢失或费用异常立即回滚。
- **Responses/WebSocket/工具缓存**：CPA 直连继续验证普通 `previous_response_id`；Sub2API HTTP 的工具续接使用显式 `function_call` + `function_call_output` replay，因为该路径对 tool-result `previous_response_id` 的既有合同是 400 且仅声明 Responses WebSocket v2 支持。HTTP `stream:true` 是 SSE，不得代替真实 WebSocket v2 验收；只有近 24 小时存在真实 WebSocket 使用时才加入独立 WebSocket 门禁。工具/流式终态最终必须唯一且终态在最后；偶发 malformed-terminal 只允许一次完整 transient retry，第二次仍异常即阻断。任何已纳入合同的上下文丢失、工具名冲突、`call_id` 无法续接均为阻断。
- **Input item ID 清理**：加入一个合规 item ID 案例和一个大于 64 字符的合成案例，验证新版本只清理目标 ID；既有长度 45 的非法 message ID 400 继续作为已知基线，不要求升级后成功。
- **模型目录**：保存升级前 CPA 和 Sub2API 的脱敏模型 ID 集合；所有近 24 小时生产在用模型必须保留。新增模型不自动暴露为 Sub2API 可售模型。
- **OAuth/凭据文件**：切换前后账号文件总数、类型和可调度数量必须一致，不允许因新文件命名逻辑产生重复凭据；管理接口的 `success/failed` 是包含并发流量、请求级错误和跨凭据 failover 的累计计数，只作为观测数据。`live`/`final_live` 必须校验两类计数增量都是三个非负整数且没有计数倒退，但数值大小不得单独导致失败或触发重试。认证文件的 `status_message` 只输出脱敏硬认证匹配计数；`invalid_grant`、invalid credentials、authentication error、unauthorized、no-available-account、refresh failure 或 401/403 匹配数在前后快照都必须为零。候选 `v7.2.104` 的 `live`/`final_live` 另以官方 `X-CPA-TRACE-ID` 对三个预期 `auth_index` 做最终成功覆盖：预期集合必须恰为三个唯一小写 16 位 hex 且各自支持 selected model；专用非流探针最多 12 次、绝对 30 秒，缺失/重复/未知/格式错误 Trace 或未覆盖 3/3 即阻断。`v7.2.82` legacy 回滚验收不要求该头。实际受控请求仍必须全部通过 HTTP、模型、文本和 token 门禁。
- **调度**：新版本支持 weighted round-robin，但现有配置仍为默认 round-robin；前后账号选择必须不因未配置权重而改变合同。
- **Session affinity**：当前配置为 `false`。使用真实 Codex session signal、`prompt_cache_key` 或等价脱敏会话标识执行多请求和工具续接，验证升级后仍遵守“无粘性时由 round-robin 选择”的现有合同，且不增加 invalid credential/no available account。
- **Trace 隐私**：`X-CPA-TRACE-ID` 只在候选直连 CPA 的受控非流探针中解析；只保留覆盖数量、探针次数和耗时，原始值不写入日志、OpenSpec 或长期证据。Sub2API localhost 与公网 Responses 成功响应的该头计数必须为零；header 缺失、重复、多 block 非最终值、未知索引或稳定索引格式错误均 fail closed。
- **插件 RPC schema**：生产没有插件目录和插件配置，升级后仍必须保持 plugins disabled，不能因默认 registry 或 schema 2 自动加载插件。
- **日志元数据**：不启用 debug；检查新版本没有显著扩大日志速率、磁盘增长或敏感元数据暴露。Authorization、Cookie、API key、完整请求体和原始会话标识不得出现在新增 metadata 日志字段中；受控日志请求携带固定的非敏感 body/session canary，门禁检查 canary、已知原始 ID 前缀和敏感字段名，且不把完整日志内容写入证据。

## Risks / Trade-offs

- [目标 release 仍较新，公开样本有限] → 要求至少 24 小时发布龄、复核官方 release/issue/PR；出现撤回或高严重度回归则停止。
- [22 个补丁版本导致影响面较大] → 固定二进制单变量升级，并把 token、WebSocket、工具、OAuth、模型目录和日志设为独立门禁。
- [默认 Linux 资产引入 CGO/plugin host 依赖] → 离线检查 ELF、架构和 GLIBC；不启用插件，不使用不同构建形态绕过失败。
- [token accounting 变化影响 Sub2API 计费] → 使用同口径真实请求和数据库/ops 聚合比较，任何结构性偏差均回滚。
- [systemd 切换造成短暂失败] → 所有下载和校验在停服前完成；采用同文件系统原子替换，停机上限 60 秒。
- [模型目录或工具合同改变] → 保存生产在用集合并回放自定义工具、namespace（当前生产工具合同预期为空）和 continuation；namespace 缺失或意外出现、工具字段不可解析即回滚。
- [OAuth 刷新状态变化] → 不并行启动候选，切换前排除登录/device flow，正常回滚不恢复旧 OAuth 快照。
- [错误日志包含完整请求体且当前权限偏宽] → 本次不混改日志策略；证据只读取脱敏字段。日志权限加固单独立项，不阻塞二进制升级，但必须记录风险。
- [准备材料写入生产磁盘] → 仅使用 `/opt/cpa/backups` 与 `/opt/cpa/staging` 的明确版本目录，root-only，校验剩余空间，不覆盖已有材料。
- [并行发布导致归因困难] → CPA 准备、切换及观察窗口与任何 Sub2API/Nginx 重建、镜像替换或配置发布互斥，并记录切换前 Sub2API 当前运行摘要。
- [短样本 success 计数无法归因到最终账号] → 候选版本使用有界 Trace 覆盖探针；保留计数结构与最终 HTTP/模型/文本/token 门禁，不以旧版本累计 delta 猜测账号归属。
- [稳定 Trace 索引被透传或落日志] → 直连只存 0600 临时文件，Sub2API/public Responses 做 header-absence 门禁，日志 fail-closed 模式匹配 Trace 名称和值。

## Migration Plan

### 阶段 0：发布与目标门禁

1. 复核 official latest 仍为 `v7.2.104`，release 未撤回。
2. 记录 official `published_at` 并确认已发布至少 24 小时；不足 24 小时不得创建 production backup/staging。
3. 复核默认 Linux AMD64 资产名、release digest、`checksums.txt` 和固定 SHA-256；官方未提供 detached signature，因此完整性信任边界固定为 GitHub release 元数据与 checksums 的一致性。
4. 检查官方 issue/PR 中与 Responses、WebSocket、工具调用、OAuth、token accounting 和启动兼容相关的高严重度回归，特别审计 GitTokenStore 数据保护、首 SSE chunk、非流式工具 item ID、WebSocket `1009` 和重试头传播。
5. 如出现新 latest，只记录，不自动改变批准目标。

### 阶段 1：生产基线

1. 记录 UTC 时间、主机架构/GLIBC、磁盘、service 状态、PID/重启数、ExecStart、当前版本/哈希、`8317/8080` 监听。
2. 记录 30 分钟同口径请求量、成功/4xx/5xx、认证错误、P95 总时长、P95 TTFT、input/cached/output tokens。
3. 导出脱敏模型集合和近 24 小时生产在用模型；核对 Responses、Chat、WebSocket/compact、自定义工具及 OAuth 登录活动。
4. 确认未启用 GitTokenStore、`plugins.sync-revision` 旧键或其他会在新版改变数据同步行为的配置；若启用则停止并单独评审。
5. 保存直连 CPA、Sub2API 和公网入口的脱敏稳定信号基线。
6. 单独记录既有 `input[6].id` 非法 message ID 400，避免把它计为升级回归。

### 阶段 2：切换前备份和候选准备

1. 解析正式二进制、配置、auth-dir、unit/drop-in 和 EnvironmentFile 路径，但不输出敏感值。
2. 创建 root-only `/opt/cpa/backups/cpa-v7.2.82-<UTC>/`，备份当前二进制、配置、unit 元数据和哈希。
3. 验证备份二进制版本、SHA-256、owner/mode 和可执行性，固定为唯一首选回滚源。
4. 下载候选至 `/opt/cpa/staging/v7.2.104/`，校验 release digest、checksums 和本地摘要。
5. 解压并验证 Linux AMD64、GLIBC 兼容性、版本首行、owner/mode；不启动候选服务。
6. 准备经审查的切换和自动回滚命令，但不执行。
7. 记录 Sub2API 当前运行镜像/digest 或等价不可变标识，并声明准备、切换和观察窗口内禁止其他发布替换 Sub2API/Nginx。

### 阶段 3：等待用户确认

1. 汇总全部门禁为通过/失败/未验证，执行只读 Review。
2. 确认生产仍为 active `v7.2.82`、正式二进制未改变、`8317/8080` 正常监听。
3. 向用户报告候选摘要、备份位置的脱敏标识、基线指标、风险和预计停机。
4. 暂停；没有用户明确确认不得进入阶段 4。

### 阶段 4：受控切换（仅在用户确认后）

1. 复核低流量窗口、无进行中的 OAuth 登录/device flow，并再次确认候选和回滚摘要。
2. 停止 `cpa.service`，确认 inactive、旧 PID 退出且 `8317` 无监听。
3. 创建 OAuth 一致性快照；复制后递归强制并验证 root-only 权限、拒绝 symlink/特殊文件，并为全部普通数据文件生成和校验不回显内容的递归 manifest。
4. 将候选安装到正式路径同文件系统临时文件，设置 `root:root`、`0755`，原子替换正式二进制。
5. 启动服务，在从首次停止开始的 30 秒绝对 deadline 内验证 active、监听、摘要和精确版本行；停止、启动、检查命令均受同一 deadline 约束，不能仅在结束后追溯耗时。
6. 控制器先在 root-only `/run/cpa-v104-switch/` 的共享锁内确认正式服务仍是精确 `v7.2.82`，再创建唯一 run ID owner token，并在立即验收和 15/30 分钟短观察完成前持续持有；已是其他版本时不得创建 owner，避免误重复运行触发降级。第二个控制器发现不同 owner 时不得切换、释放或回滚第一个控制器的窗口。
7. 远端 claim/release/switch/rollback 入口持有同一 root-only `flock`；SSH 断开触发的远端 HUP 回滚必须先释放该锁，独立回滚才可继续，避免并发 stop/install/start。认领结果不确定且 owner 匹配时，当前 run ID 采用保守回滚；认领未确认且 owner 不存在时不执行回滚。控制器已确认 owner 或已开始 release 后，owner 意外缺失视为歧义并保守回滚；不同 owner 返回只读拒绝状态，不执行生产变更。
8. 当前 unit 的 start/stop 默认超时均为 90 秒，因此脚本使用 `systemctl --no-block` 和绝对 deadline 轮询；停止 5 秒仍未完成时，按经审查策略向 `control-group` 发送 `SIGKILL`，确认 PID/监听退出后才替换二进制。
9. 在立即业务门禁和 15/30 分钟短观察完成前保持本地自动回滚控制器 armed；远端进程切换或本地控制器收到 `EXIT`、`INT`、`TERM`、`HUP`，以及任一门禁失败时，使用预留的恢复时间恢复固定 `v7.2.82` 并验证 active、`8317`、摘要、`ExecStart`、unit/drop-in 和 `/proc/$MainPID/exe`。进程切换失败时从首次停止开始到恢复的总不可用时间不得超过 60 秒。

### 阶段 5：立即验收和观察

1. 检查启动日志、模型集合、OAuth 账号池和插件禁用状态。
2. 回放直连 CPA、Sub2API、公网、Responses、Chat、WebSocket/compact、自定义工具、item ID 和 token accounting 案例。
3. 对 SSE 与非 SSE 各执行一次自定义工具调用和显式 tool-result replay，并执行长历史 Responses 以比较 replay/TTFT；CPA 直连单独验证普通 `previous_response_id`。不得把 Sub2API HTTP/SSE 上已知不支持的 tool-result `previous_response_id` 当作 200 门禁；若近 24 小时存在 Responses WebSocket v2、Codex Live/sideband/WebRTC 或图像重 payload，则加入对应真实协议的强制前后验证，否则记录未发现真实使用。
4. 关键案例最多重试一次；共享重试预算只适用于已知 5xx/transport transient 和受限 malformed-terminal。全局 `success/failed` 增量不触发重试；负数、非整数、错误长度或非法 JSON 仅作为计数结构合同失败。候选 `live`/`final_live` 的 Trace 覆盖探针另受 12 次/30 秒绝对预算约束，预算耗尽、Trace 不可解析、selected model catalog 不完整或未覆盖三个预期索引立即回滚，且不把覆盖超限转换为已有整案 transport retry。连续失败、生产模型缺失、token 结构错误、硬认证状态或账号池不可用立即回滚。
5. 观察至少 15 分钟；样本少于 20 条时延长至 30 分钟。使用同口径 Nginx 最终状态窗口，按 endpoint/status 分别比较总请求数、计数、比率和样本量。新增或未分类 5xx 不得超过 1%；既有 signature 不得超过“刷新后基线比率加 2 个百分点”和“刷新后基线比率的 1.5 倍”两者中的较大值（上限 100%），且该容差不能覆盖关键案例连续两次失败。P95 总时长和 TTFT 相对同口径基线增幅不得超过 20%。
6. 对相同请求量口径比较新增日志字节/请求；增幅不得超过 20%，且按当前速率推算不得触及 200MB 日志上限。
7. 短观察与最终 live/process/log 门禁全部通过后判定本次切换成功；继续保留 `v7.2.82` 回滚备份，整理或删除必须单独确认。

### 回滚

1. 停止 `cpa.service`。
2. 从固定、已验证的 backup 路径原子恢复 `v7.2.82` 正式二进制。
3. 启动服务并重新验证 active、`8317`、版本、模型目录、账号池和关键真实案例。
4. 默认保留当前配置、unit 和 OAuth 目录；OAuth 快照仅在证据证明损坏且单独确认后恢复。
5. 保存脱敏触发条件、时间线、恢复耗时和最终运行态。

## Open Questions

- 用户已在 `2026-08-02` 明确取消切换后的 24 小时观察；本次受控切换已完成，立即验收、15 分钟观察、最终 process/live/log/public-header 门禁和 matching-owner release 全部通过。
- 当前第五次尝试已证明旧版本 success delta 不能稳定归因；随后 candidate-only Trace 合同完成修复并成功用于第三次候选尝试。当前生产为已验证 `v7.2.104`；任何未来变更都必须重新授权并执行 fresh preflight。
- 如果等待期间官方发布 `v7.2.105` 或之后版本，仍保持 `v7.2.104` 候选；改变目标必须重新做 release、摘要和差异审计。
- 日志目录/文件权限加固是已识别的安全问题，但会改变生产存储行为，不与本次二进制升级混合。
