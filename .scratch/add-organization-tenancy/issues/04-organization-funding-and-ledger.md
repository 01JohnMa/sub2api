# 04 — 平台 JWT 授信与不可变账本

**What to build:** 让交互式平台管理员给组织余额池增加 USD 授信，并建立平台侧可完整审计、不可变且金额守恒的资金账本；组织管理员的脱敏读模型在后续报表票交付。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 01 — 平台组织基础与 capability 门禁

**Status:** resolved

- [x] 仅交互式平台管理员 JWT 可以给 active/disabled 组织增加正数 USD 授信；长效 Admin API Key、组织管理员、普通成员和无组织用户的资金写均返回 403 且资金不变。
- [x] 所有组织金额使用规范 decimal string 和 `DECIMAL(20,8)` 语义，非法精度、负数、非有限值和溢出在写入前失败。
- [x] 授信、余额池和不可变 posting 满足资金守恒，不修改个人余额、个人订阅、支付订单或其他组织资金。
- [x] 每次授信使用组织/操作作用域幂等键；重放先重新验证当前平台管理员授权，仍有权限且规范负载相同才返回原结果，不同负载冲突，不能向已撤权 actor 返回历史资金响应。
- [x] 平台侧可读取本组织未分配、available、reserved、consumed 汇总和完整内部审计 posting，且不修改个人余额、支付订单或其他组织资金。
- [x] 账本基础模型保留 public reason 与 internal audit reason 的分离字段，为后续组织管理员脱敏读模型提供稳定来源。
- [x] 平台组织详情 UI 提供授信确认、余额汇总和内部审计账本视图，正确处理幂等重放、负载冲突、非法金额和 403，且不显示任何可恢复 Secret。
