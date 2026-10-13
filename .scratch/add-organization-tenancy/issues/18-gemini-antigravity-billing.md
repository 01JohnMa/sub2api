# 18 — Gemini 与 Antigravity 组织计费 adapter

**What to build:** 把 Gemini generate/stream-generate 及 Antigravity aliases 接入统一组织计费和证据合同，保证别名、流式和 failover 不越过预留上界。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 11 — 成员移出与个人 Key 恢复

**Status:** resolved

- [x] Gemini generate 与 stream-generate request shapes 有明确的输出上限、模型映射、价格和 candidate ceiling 计算。
- [x] Antigravity aliases 映射到同一 execution/reservation/final evidence，不创建别名专属扣费路径。
- [x] 流式断开、超时、failed failover、provider evidence 和 Account posting 遵守统一 attempt/execution 口径。
- [x] 运行时新候选或重新定价候选不能越过 reservation snapshot；不支持的 shape dispatch 前拒绝。
- [x] 组织 Key 不回退个人余额/订阅，平台 quota、rate、RPM 和 concurrency 仍独立生效。
- [x] 每个 family/alias 通过 ceiling、结算、恢复和遥测测试后，只可在测试或非生产 gate 中独立开启 capability；生产默认关闭并需单独授权。

## Comments

- Gemini 与 Antigravity 在 plan 阶段冻结 action、模型、输出 cap、价格和候选；所有非 API-key Antigravity 候选必须预生成 payload，运行时禁止模型 fallback、签名修复、预算重写或重新定价。
- Gemini mixed Antigravity 与 `/messages` aliases 复用同一 execution/reservation/final evidence；持久 snapshot 仅保存 hash 和脱敏 identity summary，不保存 payload 或凭据。
- Focused service/handler、`cmd/server` 编译以及当前波次 service/handler race 验证通过；最终独立 review 为 Critical 0 / Important 0 / Minor 0。
