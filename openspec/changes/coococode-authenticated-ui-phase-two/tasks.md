## 1. Authenticated Baseline Gate

- [x] 1.1 Obtain safe signed-in user and administrator candidate sessions using synthetic non-production accounts. Production sessions remain a separately authorized release requirement; do not create/change production credentials or copy cookies/tokens into the candidate.
- [x] 1.2 Fill the complete grouped route matrix in `evidence.md`: role, exact route, sidebar visibility versus direct-route guard, active standard mode, unavailable simple/backend modes, public/admin feature source, safe read-only interactions, and deferred reason.
- [x] 1.3 Capture representative user/admin routes at 390 x 844, 768 x 900, and 1440 x 900; add 767/768 for DataTable consumers and 1023/1024 for shell consumers. Record document/table overflow, bounding boxes, action reachability, dialogs, console/API results, and non-sensitive screenshot disposition.
- [x] 1.4 Create stable Critical/Important/Minor issue IDs and an `issue -> role/route/width -> batch -> exact files -> observable acceptance` matrix using the same role, fixture, feature state, and width for before/after evidence.
- [x] 1.5 Freeze the first checkpoint's file list and exact candidate recipe. If no Important visible shell issue exists, require an independent no-op decision before P0B; either safe candidate role being unavailable remains a blocker.

## 2. P0A Authenticated Shell Checkpoint

- [x] 2.1 Select only baseline-justified files among `AppLayout`, `AppSidebar`, `AppHeader`, and `TablePageLayout`; the baseline selected none and froze all four shared components.
- [x] 2.2 Preserve menu order, role visibility, standard/simple/backend behavior, public/admin feature sources, custom items, scroll restoration, theme, route guards, content offsets, and mobile close behavior through a no-op decision.
- [x] 2.3 Run the existing sidebar, table-layout, and app-store suites; 3 files / 37 tests passed. No new test is required because no P0A source changed.
- [x] 2.4 Browser-verify both roles at representative 390/768/1440 widths and the 1023/1024 shell edge. An independent read-only reviewer found no Important shell issue and approved a P0A no-op before P0B; any later shared-shell edit reopens this gate.

## 3. P0B Table, Dialog, Card, And Status Checkpoint

- [x] 3.1 Select only baseline-justified files among `DataTable`, `StatCard`, `EmptyState`, `BaseDialog`, `ConfirmDialog`, and `StatusBadge`; `UI-P0B-001` froze existing `frontend/src/style.css` plus new `BaseDialog.spec.ts`, and the independently approved `UI-P0B-002` candidate checkpoint added only existing `BaseDialog.vue`.
- [x] 3.2 Preserve DataTable selection/multi-page selection/server sorting/virtualization/pagination/actions and dialog focus restoration/Escape/body-scroll lock/dismissal/confirmation/emitted events. The shared dialog implementation now uses per-instance body-lock ownership without changing templates, props, events, or consumer payloads; focused behavior tests pass.
- [x] 3.3 For a global shared change, enumerate all consumers and add focused fixture tests plus read-only/feature-off visual evidence for representative Audit Log, Prompt Audit, Batch Image, Ops, and Payment consumers. Never trigger save, clear, probe, deletion, payment, refund, or callback actions.
- [x] 3.4 Browser-verify both roles at 390/768/1440 and the 767/768 table edge. Close at least one preselected Important issue with same-state before/after evidence and fix all Critical/Important review findings before P1.

## 4. P1 User High-Frequency Self-Service

- [x] 4.1 Improve only accepted issue targets on `/dashboard`, `/keys`, `/usage`, `/profile`, `/available-channels`, and `/monitor`. Baseline accepted changes only for `/keys` and `/dashboard`; the other four routes remain no-op.
- [x] 4.2 Preserve requests, copy/edit/delete/export, sorting, pagination, OAuth/TOTP/password/balance settings, feature-menu sources, refresh, detail behavior, and direct-route semantics.
- [x] 4.3 Add at least one focused view/component test for every modified route and run the full frontend gate.
- [x] 4.4 Browser-verify all modified user routes at 390/768/1440 with the frozen fixtures; close the preselected Important issue(s) and complete independent review.

## 5. P2 Administrator High-Density Operations

- [x] 5.1 Improve only accepted issue targets on `/admin/dashboard`, `/admin/users`, `/admin/groups`, `/admin/channels/pricing`, `/admin/accounts`, and `/admin/usage`; “Channels” never includes Channel Monitor in this batch.
- [x] 5.2 Preserve multi-page selection, server sorting, virtualization, refresh, group/model mapping, export/cleanup, payloads, permissions, and confirmation behavior. Do not modify view business/control-flow logic, stores, API/request modules, or payload construction. The only approved view-script exception is the independently reviewed, route-local merge of existing `Column.class` presentation metadata for accepted P2 header issues; any other script change reopens the scope gate.
- [x] 5.3 Add at least one focused view/component test for every modified route and verify 390 px action/dialog reachability, 768 px wrapper-only table scrolling, and 1440 px filter/table-header bounding boxes within the first 900 px.
- [x] 5.4 Run the full frontend gate, close the preselected Important issue(s), and complete independent admin review before P3.

## 6. P3 Secondary Workflows

- [x] 6.1 P3-user: independently scope and review `/subscriptions`, `/redeem`, and `/affiliate`; preserve simple-mode guards, public feature flags, requests, forms, and direct-route behavior. The review kept Subscriptions and Redeem no-op and froze only the accepted Affiliate template/test pair; its static/unit/build and exact-candidate browser checkpoints passed.
- [x] 6.2 P3-admin: independently scope and review `/admin/subscriptions`, `/admin/announcements`, `/admin/proxies`, `/admin/redeem`, `/admin/promo-codes`, and all three `/admin/affiliates/*` routes. The independent route/source/test audit found no preselected Important issue, so all eight routes remain a reviewed no-op with no frontend file frozen.
- [x] 6.3 The sole modified route, `/affiliate`, has a focused long-value/copy-payload test and same-state 390/768/1440 geometry. Its two copy actions and feature-off direct-route behavior passed; filtering, pagination, forms, dialogs, and confirmations do not exist on this route. `UI-P3U-AFF-001` closed and final review returned `APPROVE_P3`.
- [x] 6.4 Keep `/admin/settings`, `/custom/:id`, and all deferred/high-risk routes out of P3. The final implementation-checkpoint diff review confirmed that no Settings, Custom, deferred/high-risk, script, router, store, API, request, or payload file changed; any future expansion still requires renewed scope review.

## 7. Deferred Feature-Surface Decisions

- [ ] 7.1 Audit `/admin/audit-logs` separately with TOTP step-up and clear-log behavior protected by tests; do not execute destructive production actions.
- [ ] 7.2 Audit `/admin/risk-control` and `/admin/prompt-audit` separately with risk control on/off, blocking confirmation, configuration, probe, and deletion boundaries.
- [ ] 7.3 Audit `/batch-image` separately with eligible/ineligible account/group states and preserve current menu-versus-direct-route semantics.
- [ ] 7.4 Audit `/admin/ops`, `/admin/channels/monitor`, all user/admin Payment routes, `/admin/settings`, and `/custom/:id` in separate changes with their distinct feature/config sources and safe fixture data.
- [x] 7.5 Resolve `UI-DEFER-WEBSEARCH-001` in a separate backend/config-contract change: missing `web_search_emulation_config` must either return a disabled empty configuration or have an explicitly owned caller fallback, with focused service/handler tests and no unowned console/API failure. Implemented by `fix-web-search-emulation-config-contract` at `780d04af2`: the absent setting now returns the disabled empty configuration, real storage errors remain errors through the short-lived cache, focused tests and the complete service/admin-handler package tests pass, and the exact candidate browser/log window is clean.

## 8. Per-Batch Source And Verification Gate

- [x] 8.1 Before editing, record the frozen files and run `git diff --name-status ecb7c2e3ae22686ffbcf0fac4e7a1ba98aeafcb9...HEAD -- frontend/`; inspect each frozen file with `git diff --stat 27f094e0960ebd8e52de7ff7e763c6fec2ff4057...HEAD -- <file>`. A pre-reviewed wrapper listed in the issue/file matrix is allowed; any unlisted new file, view-script change, or router/store/API/request/payload change stops work for renewed review.
- [x] 8.2 Run the exact focused/full Vitest, lint, typecheck, and build binaries and record paths, counts, warnings, and exit status. The host pnpm 11 wrapper refused the existing module store before executing tests, so the already-installed package binaries were used without reinstalling; Docker remains pinned to pnpm 9/frozen lockfile.
- [x] 8.3 Run production-dependency audit, validate repository exceptions, and run strict OpenSpec validation. Result: 0 high / 0 critical vulnerabilities, exceptions valid, strict validation passed.
- [ ] 8.4 Build an exact-commit `linux/amd64` candidate and start it with the recorded literal `docker compose -f <isolated-compose> --env-file <non-repository-secret-env> up -d --wait`; require `curl -fsS http://127.0.0.1:18080/health` -> `{"status":"ok"}`, healthy disposable PostgreSQL/Redis, synthetic accounts, no printed secrets, and a recorded teardown.
- [x] 8.5 Browser-verify every modified route for the batch's role at 390/768/1440; P0 uses both roles, breakpoint-edge checks apply to changed shared semantics, and shared cross-role consumers receive representative checks. Require zero newly introduced or unowned console/API errors and zero unexplained document overflow. The pre-recorded Ops page overflow remains explicitly deferred and was not introduced by P0B. P1 `/keys` and `/dashboard` passed their three-width checks and clean final browser/server windows. P2 passed 390/639/640/767/768/1440 geometry and safe interaction checks; its reproducible, pre-existing Web Search config `404` was owned by `UI-DEFER-WEBSEARCH-001` and is now closed by exact candidate `780d04a`. P3 `/affiliate` passed 390/768/1440 geometry, both copy actions, direct-route preservation, and a 33-response reload window with zero HTTP/load/console failures.
- [x] 8.6 Complete a read-only review against the requirement, frozen issue/file matrix, diff, tests, candidate logs, and browser evidence; fix all Critical/Important findings and record accepted Minor debt. P0B final result: 0 Critical / 0 Important / 0 Minor, `APPROVE_P0B`; P1 final result: 0 Critical / 0 Important / 0 Minor, `APPROVE_P1`; P2 final result: 0 Critical / 0 Important / 0 Minor, `APPROVE_P2`; P3 final result: 0 Critical / 0 Important / 0 Minor, `APPROVE_P3`.

## 9. Per-Batch Release Gate

- [x] 9.1 Publish `.github/workflows/coococode-image.yml` from the reviewed exact commit and record the tag, OCI index digest, runnable `linux/amd64` manifest, and embedded commit/version.
- [ ] 9.2 In `evidence.md`, record the actual production backup path, current image/digest, candidate image/digest, exact health requests/statuses, log window, migration/invalid-index state, executable application rollback, and post-rollback health check using the established v0.1.162 procedure.
- [ ] 9.3 Replace only `sub2api` with `docker compose -f /opt/sub2api/docker-compose.yml -f /opt/sub2api/docker-compose.override.yml up -d --no-deps --force-recreate sub2api`; do not restart PostgreSQL, Redis, CPA, or Nginx.
- [ ] 9.4 Verify container/public health, restart count, startup and smoke logs, representative API requests, and the batch's signed-in role routes. Roll back immediately on a Critical failure; documentation-only commits do not cause a production replacement.
