## Context

The current coococode branch is `db290d77`, with common ancestor `c32e29ba` against official Sub2API. The fork has eight unique commits and the official line has advanced to stable tag `v0.1.162`; both sides changed authenticated frontend, dependency, Docker, and gateway-adjacent files. A read-only merge preview already identifies conflicts, so a direct image swap or blind merge would either remove the custom frontend or risk losing upstream behavior.

The frontend is built and embedded into the Go release binary. Source integration, frontend verification, backend verification, image traceability, and production rollback therefore form one upgrade boundary.

## Goals / Non-Goals

**Goals:**

- Integrate the immutable official tag `v0.1.162` on an isolated branch based on `db290d77`.
- Preserve coococode branding, authenticated shell behavior, responsive table/usage fixes, security dependency remediation, and current release controls.
- Prefer upstream `v0.1.162` business logic when resolving conflicts, then reapply the smallest visual or compatibility delta needed for coococode.
- Verify source, build, authenticated UI, gateway health, and rollback evidence before production replacement.
- Produce a clean baseline for the separate authenticated UI phase-two change.

**Non-Goals:**

- No page-by-page phase-two redesign in this change.
- No independent backend API, schema, data migration, authentication, authorization, billing, or routing redesign.
- No CPA, Nginx, account-pool, credential, PostgreSQL, or Redis configuration change.
- No unrelated production configuration change. A documented temporary migration-timeout override is permitted when required to apply the tagged upstream migrations safely.
- No direct tracking of untagged `upstream/main` behavior beyond information needed to understand the stable tag.

## Decisions

- **Merge the stable tag, not `main`.** `v0.1.162` is immutable and release-oriented, while `main` contains additional unpublished commits. Alternative considered: follow `upstream/main`; rejected because it expands regression scope and weakens release traceability.
- **Use an isolated worktree and upgrade branch.** The main worktree contains user-owned uncommitted files. Alternative considered: merge in place; rejected because it risks overwriting or confusing unrelated work.
- **Resolve conflicts by behavior ownership.** Upstream wins for business logic, API interaction, state, validation, security, and new features. Coococode wins only for brand presentation and already verified compatibility fixes, adapted to the new upstream structure. Each non-trivial conflict receives a recorded decision.
- **Separate integration from phase-two redesign.** The upgrade is validated before structural page changes begin. Alternative considered: resolve conflicts while redesigning pages; rejected because failures could not be attributed to upstream integration versus new UI work.
- **Retain existing component contracts by default.** Route names, guards, store calls, emitted events, i18n behavior, table sorting/persistence, dialog accessibility, and feature flags remain upstream-compatible unless `v0.1.162` explicitly changes them.
- **Build a custom image from the reviewed commit.** Deploying the official image directly would remove the embedded coococode frontend. The candidate image must carry the exact commit and immutable digest.

## Risks / Trade-offs

- **Upstream and custom edits overlap in high-reuse frontend files** -> Resolve and test layout, table, dialog, usage, auth, dependency, and Docker clusters separately.
- **A conflict can compile while silently dropping upstream behavior** -> Compare each resolution against both parents and add focused tests for retained upstream features and custom fixes.
- **Dependency and Docker resolutions can weaken the security baseline** -> Preserve the vendored SheetJS integrity check unless an equally verified upstream replacement is adopted; run audit and image build checks.
- **Production already contains migrations 151-172 while the running source predates them** -> Verify their stored checksums against `v0.1.162` and treat the live database, not the running source tree, as migration truth. The current checksums match the tag.
- **Pending concurrent indexes can exceed the default 60-second migration timeout** -> Run the candidate against an isolated production-shaped database, verify no invalid indexes, and use a documented `SETUP_MIGRATION_TIMEOUT_SECONDS=900` cutover override if timing evidence requires it.
- **Forwarded-IP trust defaults and the tagged compatibility migration changed upstream** -> Treat an existing stored value as operator-controlled and preserve production `api_key_acl_trust_forwarded_ip=false`; do not enable forwarded-header trust until Cloudflare/Nginx header sanitization and trusted-proxy configuration are separately proven.
- **Tagged migrations change selected billing, media, audit, and auth-cache behavior** -> Verify account long-context billing flags, group media controls, admin audit retention, prompt-audit disabled state, and auth cache invalidation after migration without changing business settings opportunistically.
- **Authenticated browser coverage can be blocked by session access** -> Complete all source checks first, then pause before deployment if a safe signed-in user/admin session cannot be obtained.
- **Production data migrations may exist upstream** -> Inspect release migrations and startup behavior; stop before deployment if an irreversible or unclear migration is required.
- **The upgrade may change the phase-two UI scope** -> Re-audit target pages after the stable integration rather than replaying the old plan unchanged.

## Migration Plan

1. Record branch, tag, merge base, worktree status, live image, rollback digest, representative pre-upgrade behavior, live migration filenames/checksums, invalid-index state, and relevant table sizes.
2. Merge tag `v0.1.162` into the isolated branch without committing until conflicts are reviewed.
3. Resolve conflicts in clusters: build/dependency; layout/brand/auth; tables/usage; remaining backend and tests.
4. Inspect the combined diff against both `db290d77` and `v0.1.162`; run focused checks after each cluster.
5. Run complete frontend lint, typecheck, tests, build, backend tests, OpenSpec validation, and an independent read-only review.
6. Build and publish the exact reviewed commit; record tag, architecture, and immutable digest.
7. Verify representative signed-in user/admin routes and critical public/API behavior before cutover. Confirm the candidate applies only migrations missing from production and completes within the approved timeout.
8. Back up current production configuration and database, record the running image, preserve the explicit forwarded-IP setting, and replace only the `sub2api` service.
9. Verify health, logs, public/API behavior, and representative signed-in routes. Restore the prior image immediately if a critical check fails.

## Open Questions

- The current production image, digest, and migration state must be refreshed immediately before deployment; historical OpenSpec values are not sufficient.
- If `v0.1.162` requires a destructive or irreversible database migration, deployment requires a separate explicit decision and database rollback contract.
