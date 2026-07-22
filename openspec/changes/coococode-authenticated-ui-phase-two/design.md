## Context

Phase two begins on merge commit `ecb7c2e3ae22686ffbcf0fac4e7a1ba98aeafcb9`, which integrates official `v0.1.162` with the coococode brand and responsive fixes. Production runs the corresponding immutable image `ghcr.io/01johnma/sub2api:ui-redesign-ecb7c2e@sha256:2f7cc7bcdf2cc4b92ca79125b8417bf5a72c72c0c39433d000bd9d05396caf6e`.

Public and authentication surfaces already carry the brand strongly. Signed-in pages use the new shell and shared tokens, but their page-level hierarchy, responsive action bars, table density, dialogs, loading/empty states, and feature-gated navigation have not received a complete real-session visual pass. The work must start from authenticated evidence and measurable visible problems.

## Goals / Non-Goals

**Goals:**

- Make selected user and admin pages visibly cohesive, compact, and usable across phone, tablet, desktop, and the existing semantic breakpoint edges.
- Preserve high-density operational use: filters, tables, bulk actions, dialogs, and status signals remain scannable and reachable.
- Work in small batches whose issue IDs, files, shared consumers, tests, and before/after evidence are frozen before editing.
- Preserve upstream compatibility by styling outer structure and opt-in presentation variants before changing upstream-owned component internals.

**Non-Goals:**

- No backend, API DTO, database, storage, authentication, authorization, route guard/meta, billing, gateway, or production-setting changes.
- No changes to Vue scripts, stores, API/request modules, payload construction, event semantics, validation, save order, or destructive behavior unless a new reviewed scope explicitly authorizes them.
- No new frontend framework, state library, router, build system, or broad runtime UI dependency.
- No general-batch redesign of Settings, custom pages, Channel Monitor admin, Audit Log, Risk Control, Prompt Audit, Batch Image, Ops, or Payment.
- No use of production credentials, cookies, tokens, or sensitive records in a local or remote candidate.

## Decisions

### 1. Baseline evidence is a hard implementation gate

Before P0A, obtain authorized user and administrator sessions against a production-shaped candidate. Candidate sessions use synthetic non-production accounts and fixtures. A production session may be used only for user-authorized, read-only baseline screenshots; it is never exported or copied. If no safe session exists, P0A does not start.

All baseline and batch results are recorded in `openspec/changes/coococode-authenticated-ui-phase-two/evidence.md`. Each row includes issue ID, severity, role, exact route, simple/backend mode, feature state, viewport, candidate/base commit, non-sensitive fixture identity, screenshot reference, overflow/geometry result, console/API result, owning batch, frozen files, and disposition.

For every modified route or component, before and after evidence SHALL use the same role, fixture/data shape, feature state, and viewport. Each implementation batch must close at least one preselected Important visual issue. If the baseline yields no Important issue for a proposed batch, that batch is a no-op until a separately reviewed measurable objective is accepted.

### 2. Complete authenticated route matrix

The matrix below records current source behavior; visual work preserves the distinction between sidebar visibility and direct-route guards.

| Surface | Exact routes | Role and mode/feature source | Phase-two disposition |
| --- | --- | --- | --- |
| User core | `/dashboard`, `/keys`, `/usage`, `/profile`, `/available-channels`, `/monitor` | Authenticated user; backend mode blocks non-admin protected routes. Simple mode hides Usage and Available Channels from the menu but does not add a direct-route guard. Available Channels uses public `available_channels_enabled`; Channel Status uses public `channel_monitor_enabled`. | P1 |
| User secondary | `/subscriptions`, `/redeem`, `/affiliate` | Authenticated user. Simple mode guards Subscriptions/Redeem and hides all three menu items; Affiliate additionally uses public `affiliate_enabled` for menu visibility. Existing direct-route behavior remains authoritative. | P3-user |
| User custom | `/custom/:id` | Authenticated user/admin; entries and visibility come from public/admin custom-menu settings. | Deferred settings-driven surface |
| Batch Image | `/batch-image` and `/docs/batch-image` alias | Authenticated user; menu depends on eligible Gemini account/group access and is hidden in simple mode, while current direct-route semantics differ. | Deferred access review |
| User payment | `/purchase`, `/orders`, `/payment/qrcode`; public callbacks/results `/payment/result`, `/payment/stripe`, `/payment/airwallex`, `/payment/stripe-popup` | Authenticated routes use public `payment_enabled`; provider/result routes have distinct public/auth behavior. | Deferred commerce review |
| Admin core | `/admin/dashboard`, `/admin/users`, `/admin/groups`, `/admin/channels/pricing`, `/admin/accounts`, `/admin/usage` | Admin only; backend mode permits admins. Simple mode changes menu visibility and directly guards Groups, but does not make all hidden routes inaccessible. | P2; “Channels” means pricing view only |
| Admin secondary | `/admin/subscriptions`, `/admin/announcements`, `/admin/proxies`, `/admin/redeem`, `/admin/promo-codes`, `/admin/affiliates/invites`, `/admin/affiliates/rebates`, `/admin/affiliates/transfers` | Admin only. Simple mode guards Subscriptions/Redeem and hides several menu groups; affiliate menu uses public `affiliate_enabled`. Existing direct-route behavior remains unchanged. | P3-admin |
| Admin settings | `/admin/settings` | Admin only; owns many upstream settings fields, feature sources, validations, and payloads. | Deferred; outer shell/tab proposal requires a separate review |
| Admin channel monitor | `/admin/channels/monitor` | Admin only; menu uses public `channel_monitor_enabled`, direct route has no matching route meta flag. | Deferred feature review |
| Admin audit | `/admin/audit-logs` | Admin only; hidden from simple-mode menu; clear-log flow requires TOTP step-up. | Deferred destructive-action review |
| Admin risk/prompt audit | `/admin/risk-control`, `/admin/prompt-audit` | Admin only; route guard uses public `risk_control_enabled`; configuration, probe, blocking confirmation, and deletion are security-sensitive. | Deferred security review |
| Admin Ops | `/admin/ops` | Admin only; menu uses admin `ops_monitoring_enabled`, while direct route authorization is distinct. | Deferred operational review |
| Admin payment | `/admin/orders/dashboard`, `/admin/orders`, `/admin/orders/plans` | Admin only; route guard uses public `payment_enabled`, menu uses distinct admin payment config. | Deferred commerce review |

### 3. Freeze issue-to-batch scope before editing

Baseline issues use stable IDs such as `UI-P0A-001`. The evidence matrix maps `issue ID -> role/route/width -> batch -> exact files -> observable acceptance`. No file enters a batch merely because it appears in the initial component inventory.

- **P0A — shell checkpoint:** only baseline-justified files among `AppLayout`, `AppSidebar`, `AppHeader`, and `TablePageLayout`.
- **P0B — table/dialog/status checkpoint:** only baseline-justified files among `DataTable`, `StatCard`, `EmptyState`, `BaseDialog`, `ConfirmDialog`, and `StatusBadge`.
- **P1 — user core:** Dashboard, Keys, Usage, Profile, Available Channels, and Channel Status.
- **P2 — admin core:** Dashboard, Users, Groups, Channel Pricing (`/admin/channels/pricing`), Accounts, and Usage.
- **P3-user / P3-admin:** only the secondary routes listed in the route matrix; each sub-batch is reviewed separately.

P0A is accepted before P0B; P0B is accepted before page batches. Each checkpoint may shrink to a subset or become a no-op after baseline. Settings is not part of P3.

For each batch, record both:

```text
git diff --name-status ecb7c2e3ae22686ffbcf0fac4e7a1ba98aeafcb9...HEAD -- frontend/
git diff --stat 27f094e0960ebd8e52de7ff7e763c6fec2ff4057...HEAD -- <each-frozen-file>
```

The first command enforces the phase-two frozen scope; the second exposes how every touched upstream-owned file differs from official `v0.1.162`. A new route-local wrapper is allowed only when it was pre-listed in the reviewed issue/file matrix. Adding any unlisted file, modifying a view `<script>`, or touching router/store/API/request/payload code stops implementation and requires renewed plan review.

### 4. Shared components have an explicit blast-radius rule

“Deferred” applies to page-local visual work and business behavior; it does not pretend global components stop affecting deferred pages. Prefer opt-in variants or route-local wrappers/classes. A global P0 change must identify all consumers and provide focused fixture tests plus read-only visual regression evidence for representative consumers in Audit, Prompt Audit, Batch Image, Ops, and Payment where they use the changed component. Feature-off/redirect evidence is acceptable when the page is unavailable. No destructive, payment, probe, save, clear, or deletion action is executed.

Existing contracts remain named acceptance boundaries: DataTable selection, multi-page selection, server sorting, virtualization, pagination, and row actions; BaseDialog/ConfirmDialog focus restoration, Escape, body-scroll lock, dismissal, confirmation, and emitted events; shell route guards, feature sources, custom menus, theme, scroll restoration, and mobile close behavior.

### 5. Preserve semantic breakpoints and quantify geometry

The fixed screenshot matrix remains 390 x 844, 768 x 900, and 1440 x 900. P0A additionally checks 1023/1024 px as a pair; P0B additionally checks 767/768 px as a pair.

| Width | Measurable acceptance |
| --- | --- |
| 390 x 844 | `document.documentElement.scrollWidth === clientWidth`; sidebar is closed by default and overlay controls remain reachable; dialog `left >= 16` and `right <= innerWidth - 16`; primary actions, pagination, confirm, and cancel remain reachable. |
| 767 / 768 x 900 | The existing DataTable mode changes exactly at 768. At 767 it is card mode; at 768 it is table mode. Document width remains equal; if table content overflows, only the named table wrapper has `scrollWidth > clientWidth`, and its ancestors through the document do not. |
| 1023 / 1024 x 900 | The existing shell changes exactly at 1024. At 1023 it uses drawer/overlay semantics; at 1024 desktop sidebar/content-offset semantics apply without covering content. |
| 1440 x 900 | Expanded/collapsed sidebar does not cover content; filter/action bar and table header bounding boxes each have `top >= 0` and `bottom <= 900`; document width remains equal; compact rows and actions remain usable. |

Decorative off-canvas elements are not failures when document width remains equal. Existing errors may be allowlisted only with timestamp, route, source, reproducible evidence, and an owner. Release requires zero console errors, unhandled rejections, or failed API requests newly introduced by the batch and zero unowned/unattributed errors.

### 6. Test and candidate execution is reproducible

Every modified route/component has at least one focused view/component test. The evidence records the exact test paths and runs:

```text
cd frontend
pnpm exec vitest run <exact changed-view/component test paths>
pnpm lint:check
pnpm typecheck
pnpm test:run
pnpm build
pnpm audit --prod --audit-level=high --json > audit.json || true
cd ..
python3 tools/check_pnpm_audit_exceptions.py --audit frontend/audit.json --exceptions .github/audit-exceptions.yml
openspec validate coococode-authenticated-ui-phase-two --strict
```

The candidate uses an exact-commit `linux/amd64` image, disposable PostgreSQL/Redis, synthetic non-production user/admin accounts, and loopback binding. Each batch records its sanitized compose/env artifact paths and the literal start command; the established shape is `docker compose -f <isolated-compose> --env-file <non-repository-secret-env> up -d --wait`, followed by `curl -fsS http://127.0.0.1:18080/health`, whose expected body is `{"status":"ok"}`. The evidence also records image commit/version, container health/restart count, candidate URL, fixture seed command, and teardown result. Secret values are never committed or printed.

Browser verification is role-specific: P0A/P0B cover both roles; P1 and P3-user cover user routes; P2 and P3-admin cover admin routes. If a shared change reaches the other role, add that role's representative consumer. Every route modified by the batch is checked at the fixed three widths; breakpoint-edge pairs are added where the relevant shared semantic boundary changed.

### 7. Review and release each batch independently

Critical and Important review findings block promotion. A reviewed batch is published from its exact commit through `.github/workflows/coococode-image.yml`; record the OCI index digest and runnable `linux/amd64` manifest.

Production uses the backup, image verification, `sub2api`-only replacement, smoke, and application rollback contract already recorded under `Production Preflight And Cutover` in `openspec/changes/upgrade-sub2api-v0-1-162/evidence.md`. The replacement command remains:

```text
docker compose -f /opt/sub2api/docker-compose.yml -f /opt/sub2api/docker-compose.override.yml up -d --no-deps --force-recreate sub2api
```

Each batch evidence must record the actual backup path, old image/digest, new image/digest, exact health requests and expected statuses, log window, migration/invalid-index check, rollback command, and post-rollback health check before production execution. Documentation-only commits do not trigger a production replacement.

## Risks / Trade-offs

- **Shared CSS changes create hidden blast radius** -> Use opt-in variants first; otherwise test named consumers, including deferred high-risk surfaces, without executing mutations.
- **A large component inventory disguises little visible change** -> Only numbered baseline issues authorize files, and each batch must close an Important visible issue with same-state before/after evidence.
- **Operational density is lost to oversized branding** -> Authenticated pages use restrained accents; first-viewport geometry, row density, and filter reachability are acceptance criteria.
- **Visual changes break behavior invisibly** -> Existing selection, sorting, virtualization, focus, Escape, body lock, feature flags, and route guards are explicit contracts.
- **Production data cannot safely cover every state** -> Use synthetic fixtures for empty/error/feature-on states; production browser work is user-authorized and read-only.
- **Upstream updates conflict with broad local templates** -> Freeze files, prefer wrappers/classes, prohibit business-layer edits, and inspect each touched file relative to official `v0.1.162`.

## Migration Plan

1. Obtain safe production-shaped user/admin sessions and fill the route/feature baseline plus issue matrix in `evidence.md`.
2. Freeze and independently review P0A. Implement only its accepted issue/files, then test and browser-verify both roles including 1023/1024.
3. Freeze and independently review P0B. Implement only its accepted issue/files, then test all affected consumers including deferred surfaces and 767/768.
4. Execute P1, P2, P3-user, and P3-admin independently; do not start a page batch while its shared checkpoint is unresolved.
5. For each released batch, publish an immutable image and use the established backup/deploy/rollback procedure. High-risk surfaces require separate OpenSpec changes.

## Open Questions

- Which authorized non-production or user-approved production sessions can provide the initial user/admin read-only baseline? Without both roles, P0 remains blocked.
- Which optional features have safe representative fixtures? Disabled or unsafe features stay deferred rather than being enabled in production for visual convenience.
