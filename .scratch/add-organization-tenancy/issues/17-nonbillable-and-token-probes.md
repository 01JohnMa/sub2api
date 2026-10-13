# 17 — 非计费端点与 Token 探针

**What to build:** 为 Models、Usage 和 `count_tokens` 建立非计费组织 execution/evidence 合同；未知 provider dispatch 必须可恢复，不得创建 reservation 或组织消费 posting。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 10 — Attempt evidence、side effects 与 recovery

**Status:** resolved

- [x] Models、Usage 与 token probe 使用独立的非计费 execution/usage/token 计数模型；不创建 reservation，零 allocation 不阻断 admission。
- [x] 成功、错误和 pre-dispatch proven-zero 结果各写入唯一 zero-cost final evidence；不写组织消费 posting。
- [x] `count_tokens` 不计入 request/success/error/member-active/SLA 请求计数，但保留 input/output/cache token 汇总并可对账。
- [x] 非计费 execution 不读取个人 balance/subscription；组织 key、group、platform quota、rate、RPM 与 concurrency 语义保持独立。
- [x] unknown provider dispatch 持久化 pending attempt；通过平台管理员 JWT 受控补录 `provider_query` evidence，证据严格绑定原 pending attempt，固定 raw actual cost 为零。
- [x] pending execution 使用 reservationless list/get/recovery；resolve 只在完整证据和零费用校验通过后终结，且无 posting、支持相同幂等键 replay。
- [x] 生产 capability 默认关闭；测试/非生产 gate、durability、锁序、响应延迟提交和 recovery 路径均有验证。

## Comments

- 新增平台管理员专用的 pending evidence POST 入口，客户端不能提交 account、route、pricing 或 provider identity；这些字段从锁定的持久化 attempt 派生，响应只返回 execution/attempt/evidence ID。
- service、admin handler、route registration、repository identity 派生与 `provider_query` replay 测试通过；service/handler 与 repository race、migration regression 通过。
- 最终只读 review：Critical 0 / Important 0；Minor 保留一项响应完整缓冲的后续内存上限建议，以及由 Ticket 23 集成 gate 补充 writer→resolve→replay 串联测试。
