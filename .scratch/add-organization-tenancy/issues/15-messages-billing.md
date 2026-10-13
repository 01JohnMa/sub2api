# 15 — Messages billing

**What to build:** 为组织 key 的 `POST /v1/messages` 建立 OpenAI、Anthropic 与 Gemini 计费 adapter，复用 durable execution、attempt evidence、final evidence 与 recovery 合同，并保持生产 capability 默认关闭。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 11 — 成员移除与个人 key 恢复

**Status:** resolved

- [x] 组织 Messages 分支先于个人 subscription/legacy usage；Ticket 18 Antigravity 专用分支保持优先，middleware 仅放行 OpenAI、Anthropic、Gemini 的 `POST /v1/messages`。
- [x] 请求必须显式提供正整数 `max_tokens`，拒绝未验证的 image/document/web-search/透传/Bedrock 形态；候选计划固定一个 account，`MaximumAttempts=1`。
- [x] OpenAI 映射复用真实 Messages dispatch model 与 upstream normalization；Anthropic/Gemini 使用 tested 65000 output cap，三个 provider wrapper 均关闭隐藏 retry/continuation replay。
- [x] reservation ceiling 覆盖 input、output、cache read、cache creation 以及 Anthropic 5m/1h cache creation；actual usage 未知或可能计费的失败进入 pending reconciliation。
- [x] 只有 `ZeroCostProven && !NoHTTPResponse && writer 未变化` 才写零成本 final evidence；确定的本地 pre-dispatch 错误使用 typed zero proof。
- [x] 配置非空强制 Codex instructions 模板时排除 OpenAI OAuth candidate，避免 reservation 之后出现未冻结的输入扩张；plan 与 revalidation 使用同一门禁。
- [x] Messages HTTP/stream capability 具备 cost ceiling 且生产默认关闭；组织入口补齐 model、stream、request type 与 selected account 遥测。
- [x] service、middleware、handler 定向测试及 race、后端整包测试、Ticket 17 repository focused normal/race 与 migration regression 全部通过。

## Comments

- 最终只读 review：Critical 0 / Important 0。
- Ticket 23 集成门继续补充 Anthropic 真实 HTTP 次数回归，以及 reservation → dispatch → attempt evidence → final/pending 的端到端仓储测试。
