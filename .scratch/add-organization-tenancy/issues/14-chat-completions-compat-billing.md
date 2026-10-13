# 14 — Chat Completions compatibility 组织计费

**What to build:** 为直接 Chat Completions 路由及 Chat/Responses compatibility bridge 建立单 execution 的组织计费，避免内部协议转换产生嵌套预留或二次扣费。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 12 — Responses HTTP 与 Codex HTTP aliases 计费

**Status:** resolved

- [x] 直接 Chat Completions 的 OpenAI 与非 OpenAI platform 分支明确 stream、tools、模型映射、输出上限、倍率和最大 failover ceiling。
- [x] Chat→Responses、Responses→Chat 与 fallback 转换在一次外部请求内沿用同一 execution、reservation、candidate/pricing snapshot 和 final evidence，不创建嵌套组织扣费。
- [x] Responses HTTP 中此前 capability-gated 的 Chat fallback 只有在本票支持矩阵和测试通过后才开放。
- [x] 正常成功、部分流、下游断开、上游错误、failed failover、proven-zero 和 pending recovery 使用统一 terminalization。
- [x] 每个 attempt 的平台 Account 成本与唯一 final execution 的组织/成员扣费保持不同口径，失败 failover 不进入组织消费。
- [x] Chat family 通过协议转换、计费去重、断流和恢复测试后只可在测试或非生产 gate 中独立启用；生产默认关闭。

## Comments

- OpenAI、Anthropic 与 Gemini Chat 均使用冻结的候选、路由、模型、请求上界、价格、倍率和最大尝试数；unknown Responses 能力被拆成独立 bridge/raw attempts，组织执行中禁用隐藏 fallback 和 provider 内部重试。
- 完整 usage 的失败 attempt 先记录 Account 平台成本后才可 failover；无完整 usage 的 HTTP/transport 结果进入 pending reconciliation；发送前本地错误生成 proven-zero evidence，断流按 error 终态结算已知 usage 且不重试。
- Focused service、raw unit-tag、handler、`cmd/server` 编译以及 service/handler race 验证通过；最终独立 review 为 Critical 0 / Important 0 / Minor 0。
