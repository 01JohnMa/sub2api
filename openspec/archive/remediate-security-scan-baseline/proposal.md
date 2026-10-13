## Why

The release image built from commit `51375c8d` is blocked because the repository Security Scan reports reachable Go standard-library and AWS EventStream vulnerabilities, high-severity frontend dependency advisories, and expired `xlsx` exceptions. Production must remain on the current image until the security baseline is either remediated or explicitly risk-accepted.

## What Changes

- Upgrade the Go toolchain baseline from 1.26.4 to the minimum release that fixes `GO-2026-5856`, keeping local, CI, and Docker build versions aligned.
- Upgrade the AWS SDK dependency path to versions that fix `GO-2026-5764` without changing S3 application contracts.
- Upgrade `axios` and `form-data` to versions that clear their current high-severity production advisories.
- Investigate the two expired `xlsx` advisories and use a supported fixed release or replacement when a minimal compatible path exists; otherwise stop for an explicit risk-acceptance decision rather than silently extending exceptions.
- Require backend and frontend security scans, tests, and builds to pass before the UI release image may replace production.
- Keep API, database, authentication, authorization, billing, routing, and user-visible feature behavior unchanged.

## Capabilities

### New Capabilities

- `security-release-gate`: Defines the dependency security baseline, exception handling, release-gate evidence, and production promotion constraints.

### Modified Capabilities

No existing OpenSpec capability requirements are modified.

## Impact

- Affected code and configuration: `backend/go.mod`, `backend/go.sum`, the three release Dockerfiles, Go-version assertions in `.github/workflows/`, `frontend/package.json`, `frontend/pnpm-lock.yaml`, the pinned `frontend/vendor/xlsx-0.20.3.tgz` artifact and checksum record, `.gitignore`, and obsolete entries in `.github/audit-exceptions.yml`.
- Affected dependencies: Go toolchain, AWS SDK for Go v2, axios, form-data, and the spreadsheet export dependency path.
- APIs and data: no intended API, schema, authentication, authorization, billing, storage, or routing contract changes.
- Operations: production remains unchanged until the full CI security gate passes and a commit-addressable `linux/amd64` GHCR image is verified.
