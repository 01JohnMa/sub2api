## Why

Production Responses requests can start successfully with HTTP 200 and later end with a generic `Upstream request failed`. The client receives a protocol-level failure, but `ops_error_logs.upstream_errors` remains empty, so operators cannot distinguish a missing terminal event, a stream read failure, or an unknown forwarding failure.

## What Changes

- Record a structured, sanitized upstream-error event whenever the OpenAI Responses handler must synthesize its generic fallback response.
- Preserve the existing client contract: the client still receives a generic `response.failed` and never receives internal error text.
- Classify known post-output failures such as missing terminal events and stream read errors without inventing an upstream HTTP status.
- Reuse the existing `upstream_errors` JSON field; no database migration is required.
- Add focused regression tests for classification, sanitization, deduplication, cancellation, and HTTP-200 streaming fallback behavior.

## Impact

- Backend only: OpenAI Responses fallback handling and Ops error context.
- No changes to billing, authentication, scheduling, CPA, database schema, Nginx, Cloudflare, or public routes.
- Production deployment remains a separately authorized step.
