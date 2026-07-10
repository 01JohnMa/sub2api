## Why

生产 Sub2API 已升级到定制提交 `d601f4a`，GPT-5.6 请求、计费和图片工具兼容已可用，但 Codex Desktop 的模型目录刷新仍失败。上游 manifest handler 假设 Sub2API 自己持有 ChatGPT OAuth Token；现网则是 `Sub2API API Key upstream -> CPA OAuth pool`，所以公开 `GET /v1/models?client_version=...` 返回 `502 account has no Codex backend access token`，尽管 CPA 的同一路径已经返回包含 GPT-5.6 Sol/Terra/Luna 的 Codex manifest。

CC Switch 导入和“使用密钥”模板同时仍默认 `gpt-5.5`，且站点 Base URL 未统一包含 `/v1`。需要在保留最新上游后端和 coococode 管理界面的前提下，为 API Key 上游补充安全的 manifest 转发，并让新导入配置默认使用 GPT-5.6 Sol。

## What Changes

- 保留现有 OAuth manifest 直连逻辑；仅当所选 OpenAI 上游为 API Key 账号且没有 Codex OAuth Token 时，使用该账号的 `base_url` 与 `api_key` 请求上游 Codex manifest。
- 将 API Key 上游 manifest URL 规范化为恰好一个 `/v1/models?client_version=...`，透传必要的 Codex 版本、缓存和客户端请求头，并对无效响应安全失败。
- 将 CPA 返回的 manifest 限制到 Sub2API 账号允许的模型，避免 Desktop 展示本账号不可调度的模型；过滤后的响应不复用上游 ETag。
- 将 CC Switch OpenAI 导入 endpoint 规范化为带 `/v1` 的 URL，并把默认模型改为 `gpt-5.6-sol`。
- 将普通与 WebSocket “使用密钥”Codex 配置的 `model` 和 `review_model` 改为 `gpt-5.6-sol`。
- 从生产提交 `d601f4a` 创建隔离分支，通过现有自定义镜像工作流发布并只重建 Sub2API；失败时恢复当前镜像 digest。

## Capabilities

### New Capabilities

- `codex-upstream-compatibility`: 补充 API Key 上游后的 Codex manifest 转发、模型可见性约束和客户端默认配置合同。

### Modified Capabilities

- 无。现有品牌、账号池、认证和 CPA 行为保持不变。

## Impact

- 后端：`openai_codex_models` handler/service 及定向测试。
- 前端：CC Switch 导入工具、Use Key Codex 模板及定向测试。
- 发布：基于 `d601f4a` 的新定制镜像，只替换生产 Sub2API。
- 不涉及：CPA 二进制、PostgreSQL schema、用户/API Key、OAuth 文件、Nginx、Cloudflare。
