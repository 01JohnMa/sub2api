## Why

When `web_search_emulation_config` has never been persisted, the admin GET endpoint currently translates the repository's setting-not-found result into HTTP 404. Several existing admin callers load this optional global configuration during page or modal initialization, so a normal "not configured" state produces repeated failed requests and blocks the phase-two production release gate.

## What Changes

- Define an absent Web Search emulation setting as a valid disabled configuration with no providers.
- Return that default through the existing admin GET endpoint with HTTP 200 and the existing response envelope.
- Preserve the current saved-configuration shape, administrator API-key field behavior, quota population, update behavior, and runtime manager behavior.
- Continue returning errors for real repository/database failures; do not hide operational faults as an empty configuration.
- Add focused service and handler coverage for both the missing-setting default and genuine storage failures.

## Capabilities

### New Capabilities

- `web-search-emulation-config`: Defines the read contract for absent, configured, and storage-error Web Search emulation settings.

### Modified Capabilities

None.

## Impact

- Backend service and focused tests under `backend/internal/service`.
- Admin settings handler coverage under `backend/internal/handler/admin`.
- Existing endpoint: `GET /api/v1/admin/settings/web-search-emulation`; no route, DTO, response-envelope, authentication, or authorization change.
- No frontend source change, database migration, dependency change, production setting write, or automatic enablement of Web Search.
