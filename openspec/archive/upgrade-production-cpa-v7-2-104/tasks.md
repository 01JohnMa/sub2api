## 1. 发布目标与阻断门禁

- [x] 1.1 复核 official latest、固定 release `v7.2.104`、发布时间已满 24 小时且 release 未撤回
- [x] 1.2 复核默认 Linux AMD64 资产名、release digest、`checksums.txt` 与固定 SHA-256 完全一致
- [x] 1.3 审计 Responses、WebSocket、工具调用、OAuth、token accounting、启动兼容和 GitTokenStore 相关公开回归并记录阻断判断
- [x] 1.4 确认即使出现新 latest 也不自动改变本次固定目标

## 2. 生产运行态与数据兼容基线

- [x] 2.1 脱敏记录 UTC 时间、主机架构/GLIBC、磁盘、service/PID/重启数、ExecStart、正式版本/摘要和 `8317/8080` 监听
- [x] 2.2 解析配置结构并确认当前为普通 `round-robin`、`session-affinity=false`、plugins disabled，且不输出任何密钥或凭据内容
- [x] 2.3 确认未启用 GitTokenStore、没有 `plugins.sync-revision` 旧键；若任一存在则停止准备并单独评审
- [x] 2.4 脱敏记录 OAuth 文件总数、类型、可调度数量及近期待认证错误基线
- [x] 2.5 记录 Sub2API 当前运行镜像 digest 或等价不可变标识，并确认短时升级窗口与 Sub2API/Nginx 发布互斥
- [x] 2.6 保存最近 30 分钟请求量、成功/4xx/5xx、认证错误、P95 总时长、P95 TTFT 和 token 结构基线
- [x] 2.7 保存 CPA、Sub2API 和公网入口模型集合及近 24 小时生产在用模型的脱敏基线
- [x] 2.8 将现有长度 45 的 `input[6].id` `invalid_value` 400 记录为既有客户端合同问题

## 3. v7.2.82 正式回滚备份

- [x] 3.1 再次解析正式二进制、配置、unit/drop-in 和 EnvironmentFile 元数据路径，确认目标 backup 目录不存在且空间充足
- [x] 3.2 创建 root-only `/opt/cpa/backups/cpa-v7.2.82-<UTC>/` 并复制当前二进制、配置副本、unit/drop-in 和 EnvironmentFile 元数据
- [x] 3.3 生成脱敏清单和 SHA-256，验证备份二进制版本、owner/mode、可执行性与当前正式二进制一致
- [x] 3.4 确认既有 `v7.2.61` 二级灾备仍保留，且旧 staging 文件未被当作正式回滚源

## 4. v7.2.104 候选准备

- [x] 4.1 创建 root-only `/opt/cpa/staging/v7.2.104/` 并下载固定官方资产和 `checksums.txt`
- [x] 4.2 校验下载文件 SHA-256 为 `993babb37b6de831600f0eb31527ca0f938337e1d1f837d5cf846263affa9724`
- [x] 4.3 解压候选并验证 ELF/Linux AMD64、GLIBC 兼容性、版本首行 `v7.2.104`、owner/mode 和可执行性
- [x] 4.4 确认候选未启动、未加载生产 OAuth 目录且运行中正式二进制和 PID 均未改变
- [x] 4.5 准备并只读审查同文件系统原子切换、绝对 deadline、唯一 owner token、`EXIT/INT/TERM/HUP` 控制器和固定 `v7.2.82` 自验证自动回滚命令，不执行任何切换命令

## 5. 升级前真实案例矩阵

- [x] 5.1 对 CPA 直连、Sub2API 和公网入口各保存一次正常请求的状态、模型、非空输出、流式终止和脱敏延迟证据
- [x] 5.2 对生产在用 Responses 与 Chat 模型保存 input/cached/output/reasoning token 结构和 Sub2API 计费记录基线
- [x] 5.3 使用正常 Codex session signal 或等价脱敏标识执行多请求，记录 `session-affinity=false` 下的账号调度和上下文连续性基线
- [x] 5.4 对 SSE 与非 SSE 各完成一次自定义工具调用及 tool-result continuation，验证工具名、namespace（基线为空）、`call_id` 和参数可解析
- [x] 5.5 完成长历史 Responses replay，并记录稳定完成、总时长和 TTFT
- [x] 5.6 验证一个合规 input item ID 和一个大于 64 字符的合成 ID；保留长度 45 的既有非法 message ID 失败基线
- [x] 5.7 检查近 24 小时是否真实使用 Codex Live/sideband/WebRTC 或图像重 WebSocket；存在则加入强制案例，不存在则记录未发现且不启用新功能
- [x] 5.8 保存当前日志字节/请求和磁盘增长基线，并确认采集证据未包含 Authorization、Cookie、API key、完整 body 或原始敏感会话标识

## 6. 切换前审查与人工确认点

- [x] 6.1 汇总发布、运行态、数据兼容、备份、候选和真实案例门禁为通过/失败/未验证
- [x] 6.2 执行只读 Review，复核 OpenSpec、生产证据、切换命令、回滚命令和所有未解决风险；最终 Review 为 Critical/Important/Minor 均 0
- [x] 6.3 再次确认生产仍为 active `v7.2.82`、正式摘要未改变且 `8317/8080` 正常监听
- [x] 6.4 向用户报告候选摘要、回滚备份脱敏标识、基线、已知风险和预计停机，并暂停等待明确的切换确认
- [x] 6.5 收到用户明确确认后，复核低流量窗口、发布互斥和没有进行中的 OAuth 登录/device flow

## 6A. 失败尝试后的再次授权门

- [x] 6A.1 使用永久 `0700` 外层目录和 `auth/` 复制子目录，修复递归 owner/mode、symlink/特殊文件拒绝、`find` 错误传播、非 foreground 进程组 deadline、全数据文件 manifest 与静默自校验，并通过 Linux 正反例测试
- [x] 6A.2 （历史尝试）为 `live`/`final_live` 实现过一次 `[0,0,1]` 失败计数重试；该规则由 6A.10 明确废止，历史行为保留在失败证据中
- [x] 6A.3 重新生成 manifest/controller 自哈希，运行全部 `bash -n`、测试、`sha256sum -c`、verify-only 和 OpenSpec strict
- [x] 6A.4 执行强制只读 Review；Critical/Important/Minor 均为 0，当前 controller 仅恢复为“通过审查但等待新授权”的资格
- [x] 6A.5 刷新生产 `v7.2.82`、端口、owner、候选/回滚摘要和发布互斥证据，然后暂停等待新的明确切换授权
- [x] 6A.6 将有限 curl/SSH transport transient、`429/500/502/503/504` overload 与硬认证失败分开分类；当前 failed-counter 仅作结构化观测，不再进入重试预算
- [x] 6A.7 对工具、stream continuation 和长历史 SSE 强制唯一终态；对偶发 `dual_terminal`/非法终态顺序或数量只允许一次完整 transient retry，第二次仍异常立即阻断，并用 `ops_error_logs` 的脱敏稳定签名补齐 15/30 分钟新增/未分类错误门禁及正反例测试
- [x] 6A.8 重新生成 manifest/controller 自哈希，运行全部语法、状态机、观察、Linux snapshot、verify-only、OpenSpec strict 和强制只读 Review，再刷新生产 preflight
- [x] 6A.9 修正 Sub2API HTTP 工具续接合同：移除错误的 tool-result `previous_response_id` 200 门禁，SSE/非 SSE 均改用显式 replay，增加精确 tool-output/namespace 门禁、24 小时条件流量阻断、只读工件快照和日志 fail-closed 读取，补合同测试、第二次失败证据、重哈希和强制只读 Review
- [x] 6A.10 根据旧版本反例修正 OAuth 计数合同：移除不可归因的全局 `failed` 数值 pass/fail 与重试门禁，新增 `status_message` 硬认证计数（只保留计数、不输出正文），保留三元组非负/无倒退结构校验，补 `[0,1,1]`、`[2,7,3]`、非法结构和硬认证 fixture 测试，更新 spec/design/runbook/证据，重建 manifest/controller 自哈希并完成只读 Review
- [x] 6A.11 使用 v7.2.104 candidate-only `X-CPA-TRACE-ID` 替代不可归因的逐账号 success-delta 门禁：验证三个 selected-model catalog、12 次/30 秒绝对预算内覆盖 3/3 最终成功匿名索引、v7.2.82 legacy 兼容、Sub2API localhost/public 不透传、日志不落 Trace，并完成正反例测试、manifest/self-hash、verify-only、OpenSpec strict 和强制只读 Review

## 7. 受控切换（仅限用户明确确认后）

- [x] 7.1 停止 `cpa.service`，确认旧 PID 退出、service inactive 且 `8317` 无监听
- [x] 7.2 在停服后创建 OAuth 一致性快照和不回显内容的摘要清单；发现 `cp -a` 将根目录降为 `0755` 且原 manifest 漏掉嵌套文件后，已将本次快照目录/文件分别收紧为 `0700`/`0600`，递归覆盖 4 个数据文件重建 manifest 并静默校验通过
- [x] 7.3 将候选复制到正式路径同文件系统临时文件，设置 `root:root`、`0755` 并原子替换正式二进制
- [x] 7.4 启动服务并在 30 秒内验证 active、`8317` 监听和版本 `v7.2.104`，总停机不超过 60 秒
- [x] 7.5 任一进程门禁失败时自动从固定 backup 原子恢复 `v7.2.82` 并验证恢复运行态（本次进程门禁通过；后续业务门禁失败触发的同一自动回滚路径已实际验证）

## 8. 立即验收与观察

- [x] 8.1 验证启动日志、模型集合、OAuth 文件/可调度数量、round-robin 合同和 plugins disabled
- [x] 8.2 回放阶段 5 的 CPA、Sub2API、公网、Responses、Chat、SSE/非 SSE 工具、长历史和 item ID 案例
- [x] 8.3 对比 token accounting 与 Sub2API 计费，确认无负数、重复计量、字段丢失或异常费用
- [x] 8.4 对比真实会话序列，确认上下文连续且 `session-affinity=false` 未被隐式改成账号粘性
- [x] 8.5 脱敏抽样新增日志 metadata，确认无敏感数据暴露且日志字节/请求增幅不超过 20%
- [x] 8.6 观察至少 15 分钟；样本少于 20 条则延长至 30 分钟；按 endpoint/status 比较总量、计数、比率和样本，验证新增/未分类 5xx 不超过 1%、既有 signature 未越过基线容差且 P95/TTFT 增幅不超过 20%
- [x] 8.7 用户已明确取消切换后的 24 小时观察；立即验收、适用 15/30 分钟短观察、最终 live/process/log 门禁和 matching-owner release 全部通过后才可判定本次切换成功

## 9. 回滚与收尾

- [x] 9.1 若触发回滚，停止候选并从固定 backup 原子恢复 `v7.2.82`，不覆盖当前 OAuth 目录
- [x] 9.2 回滚后重新验证 active、`8317`、版本、模型、账号池和关键真实案例
- [x] 9.3 保存脱敏触发条件、时间线、恢复耗时和最终运行态
- [x] 9.4 短观察成功后确认固定 `v7.2.82` 回滚备份仍存在且摘要一致，继续保留，不执行整理或删除
