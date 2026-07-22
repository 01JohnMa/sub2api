## Why

The coococode production fork is based on an older Sub2API snapshot while the official stable release has advanced to `v0.1.162` and has changed the same authenticated frontend and gateway areas targeted by the next UI phase. Integrating the stable release first avoids building new page redesign work on stale business logic and reduces repeated conflict resolution.

## What Changes

- Integrate official Sub2API tag `v0.1.162` into the current coococode production-source branch in an isolated upgrade branch.
- Preserve the existing coococode brand, authenticated shell behavior, responsive table fixes, usage fixes, security baseline, and release/rollback workflow while accepting upstream business behavior by default.
- Resolve source, dependency, Docker, test, and frontend conflicts with an explicit per-file decision record.
- Re-run backend and frontend verification, then use an authenticated browser session to validate representative user and admin routes before any production replacement.
- Keep the authenticated page-by-page phase-two redesign out of this change; it will be re-scoped against the upgraded baseline.
- Do not change CPA, Nginx, database data, account-pool configuration, credentials, permissions, billing contracts, or production routing as part of the source integration.

## Capabilities

### New Capabilities

- `upstream-release-integration`: Defines traceable stable-tag integration, coococode customization preservation, regression verification, and reversible release requirements.

### Modified Capabilities

None.

## Impact

- Affected code: files changed by the merge of `v0.1.162`, especially frontend layouts, shared tables/dialogs, authenticated views, dependencies, Docker build files, and tests.
- APIs and data: upstream `v0.1.162` behavior is adopted, but this change introduces no independent API, schema, authentication, authorization, billing, or data migration contract.
- Delivery: the release must be built from the reviewed integration commit, verified by immutable image digest, and deployed only with the current production image recorded for rollback.
- Follow-up: the phase-two authenticated UI plan must be re-audited after this integration because upstream already modified its target pages.
