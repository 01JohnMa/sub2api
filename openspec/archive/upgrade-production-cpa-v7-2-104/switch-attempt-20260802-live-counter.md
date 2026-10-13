# v7.2.104 Live Counter Contract Remediation

## Trigger

The fourth controlled `v7.2.104` attempt reached the business gate and then rolled back on `live_consecutive_or_changed_failure`. The rollback was verified, and production remained on the fixed `v7.2.82` binary. A read-only replay on the unchanged `v7.2.82` path produced successful direct CPA, Responses continuation, Chat, and round-robin requests while the sorted cumulative `failed` delta was `[0,1,1]` in one sample. This demonstrates that the management counter includes concurrent or request-scoped outcomes and cannot be attributed to the controlled case.

## Observed Contract

- `GET /v0/management/auth-files` exposes aggregate `status`, `status_message`, `disabled`, `unavailable`, `runtime_only`, `success`, `failed`, and recent-request counters.
- `GET /v0/management/auth-files/models?name=...` requires a name and returns a per-credential model catalog only. It does not expose model health, `last_error`, or an authentication status.
- The current three-file production snapshot has unchanged provider/identity shape, zero disabled/unavailable/runtime-only entries, and zero redacted hard-auth matches in `status_message`.
- A fresh read-only `v7.2.82` canary passed all selected-model HTTP/text/token checks and produced success deltas `[3,3,3]`; its failed-counter delta was `[0,0,0]`. Earlier unchanged-version samples included `[0,1,1]` and other non-zero vectors without a schedulability regression.

## Remediation

The validator now treats `failed` deltas as telemetry with a strict shape contract only: exactly three non-negative integers, so counter regression, malformed JSON, non-integer values, and wrong length remain blocking. The value does not cause a retry or failure. A separate redacted hard-auth count matches only known credential/authentication signatures in `status_message`; any non-zero count before or after the controlled requests is an immediate non-retryable hard-auth failure. HTTP 401/403 and explicit hard-auth response types remain non-retryable through the existing classifier.

The malformed-terminal retry remains limited to the three established transport classes, and no third attempt is introduced. The per-credential model endpoint remains documentation-level catalog evidence; actual selected-model Responses/Chat success remains the runtime health proof.

## Verification Boundary

No production configuration, binary, OAuth file, route, database row, Sub2API container, or Nginx state was changed during this remediation. The script manifest, controller self-hash, full test suite, OpenSpec strict validation, and independent read-only Review now pass; the next production switch still requires a new explicit user authorization.

## Completed Verification

- Controller SHA-256: `b77ea68dd8b6e9512b30eddb7fa86b8c966628779ba0dca7c71d7a58617612a1`; normalized self-check: `37c7121fd9ae8366494333a9f128f3a271b4fb7868f6d5c1768f61b7353c5dba`.
- Manifest SHA-256: `0677595490a41f9b213d3c96a83b17cd3ab2ccfcd60f8df283ae8eea6be6e4ca`; `sha256sum -c`, controller verify-only, all script syntax checks, local contract suites, Linux OAuth snapshot tests, and OpenSpec strict validation passed.
- A read-only `v7.2.82` live canary and old-version-specific production preflight passed; the post-switch validator separately rejected the old version at its expected candidate SHA gate.
- The final independent read-only Review reported Critical `0`, Important `0`, and Minor `0`.

Official API reference: https://help.router-for.me/cn/management/api
