## Context

The authenticated admin frontend reads `GET /api/v1/admin/settings/web-search-emulation`
from Settings, Channels, and account create/edit flows. The endpoint exists, but a
fresh installation may not have the optional `web_search_emulation_config` setting.
The repository then returns `ErrSettingNotFound`.

The service currently treats that condition like every other storage error: it caches
an empty configuration for five seconds and returns the error. The first request is
therefore a 404, requests inside the error-cache window can unexpectedly return 200,
and the next request after expiry can return 404 again. This creates an unstable API
contract and browser warnings even though "not configured" is a valid disabled state.

## Goals / Non-Goals

### Goals

- Make an absent optional Web Search emulation setting resolve to a stable disabled
  configuration.
- Keep the existing admin response envelope and return `providers` as an empty array.
- Preserve failures for database, timeout, and other non-not-found repository errors.
- Preserve configured settings, administrator API-key field behavior, quota usage enrichment, saves,
  and runtime manager behavior.

### Non-Goals

- Changing any frontend caller or hiding unrelated frontend errors.
- Creating the setting automatically or enabling Web Search emulation by default.
- Changing the database schema, migrations, provider contracts, or quota semantics.
- Changing the package-level cache lifetime or broader retry policy.

## Decisions

### 1. Optional-default semantics belong in the service

`SettingService.loadWebSearchConfigFromDB` will recognize only
`errors.Is(err, ErrSettingNotFound)` and return:

```go
&WebSearchEmulationConfig{
    Enabled:   false,
    Providers: []WebSearchProviderConfig{},
}
```

The value will use the normal successful cache lifetime. The handler and frontend
callers remain unchanged.

This keeps one contract boundary for all current and future consumers. Handling the
404 in each frontend caller would still leave server-side error noise and would allow
the callers to drift.

### 2. Unexpected repository failures keep their existing error path

Any error other than `ErrSettingNotFound` will continue through the current
short-lived error-cache and returned-error path. The cache item retains the error so
reads within that window return the same failure instead of converting it to a
successful disabled configuration. A database outage must not be represented as a
legitimate disabled configuration.

### 3. The response collection is explicitly non-nil

The missing-setting default uses an initialized empty provider slice. This preserves
the frontend array contract and prevents `providers: null` from becoming part of the
API response.

### 4. Verification covers both service and HTTP contracts

Service tests will prove the missing-setting default and the non-not-found failure
boundary. A handler test will prove that the admin endpoint retains the existing
success envelope with HTTP 200, `enabled: false`, and `providers: []`.

## Risks / Trade-offs

- An overly broad error check could hide storage outages. The implementation and tests
  therefore match only `ErrSettingNotFound`.
- A normal cache lifetime means a setting created outside the service write path may
  remain disabled until the cache expires. This is consistent with current successful
  reads; the supported save path already refreshes the runtime configuration.
- The package-level cache requires deterministic test setup. Tests will seed an
  expired, non-nil cache entry and will not run in parallel.
- Unexpected repository errors remain cached for the existing short window, and every
  read in that window returns the cached error.

## Migration Plan

No data migration or configuration write is required.

1. Run focused service and handler unit tests.
2. Build the existing frontend and backend candidate.
3. Verify the endpoint and browser callers against that exact candidate.
4. Publish and deploy the exact reviewed image through the existing release path.
5. Roll back by restoring the previous application image and recreating only the
   `sub2api` service if production validation fails.

## Open Questions

None. The absent setting is optional and already represents a disabled runtime state.
