## 1. Baseline And Conflict Contract

- [x] 1.1 Record the coococode source commit, official tag commit, merge base, unique patch counts, dirty-worktree isolation, and read-only conflict inventory.
- [x] 1.2 Inspect `v0.1.162` release changes, migrations, Docker/dependency changes, and the current production image/health/rollback reference without changing runtime state.
- [x] 1.3 Run focused pre-merge frontend and backend checks covering coococode brand, auth, table responsiveness, usage behavior, manifest behavior, and release build inputs.

## 2. Stable Tag Integration

- [x] 2.1 Merge official tag `v0.1.162` into the isolated branch without committing and record the exact conflict set.
- [x] 2.2 Resolve Docker, dependency, lockfile, vendored SheetJS, CI, and security-baseline conflicts; verify dependency installation and build inputs.
- [x] 2.3 Resolve brand, public/auth, app-shell, route-title, locale, and global-style conflicts while preserving upstream business behavior.
- [x] 2.4 Resolve table, usage, account-display, responsive behavior, and related test conflicts against the upstream implementations.
- [x] 2.5 Resolve remaining backend, release, documentation, and test conflicts; confirm the index contains no unresolved paths.

## 3. Source Verification And Review

- [x] 3.1 Run focused tests for every conflict cluster and add or update tests where an upstream/custom behavior decision is not already protected.
- [x] 3.2 Run frontend lint check, typecheck, complete test suite, production build, and dependency/security checks.
- [x] 3.3 Run backend tests, build checks, and any migration/startup verification required by `v0.1.162`.
- [x] 3.4 Compare the resolved tree against both `db290d77` and `v0.1.162`; document preserved custom patches and adopted upstream behavior, then run strict OpenSpec validation.
- [x] 3.5 Complete an independent read-only review; fix all Critical and Important findings and rerun affected checks.

## 4. Authenticated And Release Verification

- [x] 4.1 Build and run an isolated candidate stack, verify health/startup logs, and remotely browser-check representative signed-in user/admin routes plus public/auth routes at 390, 768, and 1440 px.
- [ ] 4.2 Commit the reviewed integration with a Chinese message, push the isolated branch, verify CI publishes the exact commit image, and record its immutable digest.
- [ ] 4.3 Refresh production topology, running image, database migration state, backups, and executable rollback commands immediately before cutover.
- [ ] 4.4 Replace only the production `sub2api` image, then verify container/public health, logs, representative API requests, and signed-in user/admin behavior; roll back immediately on a critical failure.

## 5. Phase-Two Handoff

- [ ] 5.1 Re-audit authenticated routes and shared components on the upgraded baseline and create the separate phase-two frontend OpenSpec with revised page batches and visual acceptance evidence.
