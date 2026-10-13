## Context

当前生产运行 `ghcr.io/01johnma/sub2api:ui-redesign-d601f4a`，镜像 digest 为 `sha256:5d81d4e974b26c581dd10dc17267129adf0ea085ff35c1a47a5f6737a08cc3b9`；Sub2API、PostgreSQL、Redis 均 healthy，`cpa.service` active。

正常 Responses 请求使用账号 `base_url=http://host.docker.internal:8318` 和 API Key 转发到 CPA。Codex 模型目录请求则进入上游新增的特殊 handler：它从所选账号读取 ChatGPT OAuth access token，并直接请求 `https://chatgpt.com/backend-api/codex/models`。现网 Sub2API 账号是 API Key 类型，OAuth 位于 CPA 内部，因此该 handler 在发起任何 CPA 请求前返回 `OPENAI_CODEX_MODELS_TOKEN_MISSING`。

只读验证表明：

- 公开 `GET /v1/models?client_version=0.144.0` 返回 502，消息为 `account has no Codex backend access token`。
- 从 Sub2API 容器使用上游账号 API Key 请求 CPA `GET /v1/models?client_version=0.144.0`，返回 Codex manifest，并包含 Sol、Terra、Luna。
- 生产前端 CC Switch 导入固定传递 `model=gpt-5.5`；当前站点导入的 endpoint 缺少 `/v1`。

## Goals / Non-Goals

**Goals:**

- 让 API Key 类型 OpenAI 上游能够提供 Codex manifest，同时保持 OAuth 账号原有直连行为。
- 让 manifest 仍经过 Sub2API API Key 鉴权、分组和账号模型约束。
- 让 CC Switch 新导入配置使用正确 `/v1` endpoint，并默认 `gpt-5.6-sol`。
- 使用最小定向测试、只读 review、可追溯镜像和单服务回滚完成生产发布。

**Non-Goals:**

- 不修改 CPA、OAuth 池、数据库 schema、Nginx 或 Cloudflare。
- 不伪造 `auth_mode=chatgpt`、JWT、account id 或其他 Codex 客户端认证状态。
- 不修改或重新打包 Codex Desktop。
- 不全量重跑与 manifest/Key 配置无关的测试套件。

## Decisions

### 1. OAuth 优先，API Key 上游仅作受限 fallback

若账号能够解析出 Codex OAuth access token，完全保留上游现有 ChatGPT manifest 直连逻辑。只有 `AccountTypeAPIKey` 且 access token 为空时才进入 fallback；其他账号类型继续明确失败，避免认证边界扩大。

### 2. 复用账号上游配置，不引入第二份 CPA 配置

fallback 使用所选账号已有的 `base_url`、`api_key`、proxy 和 HTTP client 安全策略。URL 规范化为恰好一个 `/v1/models`，保留 `client_version`，并发送 `Authorization`、`Accept`、`Originator`、`Version`、`User-Agent`。不得硬编码 CPA 地址或密钥。

### 3. 过滤 API Key fallback 的 manifest

CPA manifest 可能包含超出当前 Sub2API 账号白名单的模型。fallback 仅保留账号允许的 `models[].slug`；账号未设置白名单时保持原响应。过滤路径不转发客户端 `If-None-Match`，也不返回上游 ETag，避免不同白名单复用同一缓存标识。OAuth 原路径继续保留 ETag/304。

### 4. 无效上游响应安全失败

fallback 必须要求 2xx JSON 且存在顶层 `models` 数组；OpenAI 标准 `{object,data}` 列表不能冒充 Codex manifest。错误响应受现有 body limit 约束，不回显 API Key。

### 5. 客户端默认值在导入源统一

CC Switch OpenAI deeplink endpoint 使用规范化后的 `/v1` Base URL，默认模型为 `gpt-5.6-sol`。Use Key 普通与 WebSocket 模板同步更新 `model` 和 `review_model`，避免同一站点产生不一致配置。

### 6. 从真实生产提交发布

实现基线是 origin `codex/upstream-coococode-ui-20260710` 的 `d601f4a`，不是旧主工作区 `abd5010`。在隔离 worktree 创建 `codex/fix-codex-manifest-cpa`，通过现有自定义镜像工作流生成 `ui-redesign-<short-sha>`。部署只重建 Sub2API，旧镜像 digest 作为回滚目标。

## Risks / Trade-offs

- [API Key 上游不支持 Codex manifest] -> 验证顶层 `models`；不支持时返回明确 502，不退化成错误格式。
- [模型白名单过滤破坏 ETag] -> fallback 禁用 ETag/304；OAuth 原路径不变。
- [Base URL 已含 `/v1`] -> 统一 URL join 测试覆盖有/无 `/v1` 和尾斜杠。
- [上游指向自身导致递归] -> 复用现有账号 URL 安全校验，并为 fallback 增加单跳保护或自指检测。
- [Desktop 仍进行客户端过滤] -> 先验证公开 manifest 与真实请求；若 Desktop 仍隐藏，不修改签名 App，停止并报告客户端限制。

## Migration Plan

1. 从 `d601f4a` 创建隔离 worktree并记录当前生产镜像、digest与健康状态。
2. 先补 API Key manifest fallback 的失败测试，再实现 fallback、校验和白名单过滤。
3. 更新 CC Switch/Use Key 默认值与前端测试。
4. 运行定向 Go/Vitest、typecheck、前端 build、diff 检查与独立只读 review。
5. 提交并推送目标分支，等待自定义镜像产出并记录 digest。
6. 只重建 Sub2API；验证 health、manifest、一次 Sol 请求和一次 5.5 请求。
7. 任一关键验收失败时恢复 `sha256:5d81d4e974b26c581dd10dc17267129adf0ea085ff35c1a47a5f6737a08cc3b9`。
