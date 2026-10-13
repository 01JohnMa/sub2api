# 16 — Embeddings 与 Images 组织计费

**What to build:** 为 Embeddings 和图像生成/编辑建立基于输入、数量和尺寸边界的组织计费，复用统一 execution/evidence 合同而不改变非计费端点语义。

**Specification:** OpenSpec change `add-organization-tenancy`

**Blocked by:** 11 — 成员移出与个人 Key 恢复

**Status:** resolved

- [x] Embeddings adapter 使用已验证的输入 Token/批量边界计算最大成本并在 dispatch 前预留。
- [x] 图像生成/编辑 adapter 使用数量、输入/输出尺寸和已验证价格快照计算最大成本。
- [x] 无法验证 count、size、model 或价格上界的 request shape 在 dispatch 前稳定拒绝。
- [x] Embeddings、Images 分别通过 ceiling、reservation、断流、失败 failover 和 evidence 对账测试后，只可在测试或非生产 capability gate 中启用；生产默认关闭并需单独授权。

## Comments

- Embeddings 与 Images 均在 reservation 前冻结候选、模型、输入/数量/尺寸上界和价格；运行时只允许快照内候选，生产 capability 默认关闭。
- Embeddings 仅在完整有效 usage 后结算；Images 仅使用实际响应图片数量和尺寸，不以请求 `n` 合成 usage。断流继续排空上游并保留已知 usage，未知 transport/usage 进入 pending reconciliation。
- Focused service/handler、`cmd/server` 编译以及当前波次 service/handler race 验证通过；最终独立 review 为 Critical 0 / Important 0 / Minor 0。
