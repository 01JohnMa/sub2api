## Why

The first coococode visual pass established the public/auth brand, shared tokens, and authenticated shell, but most signed-in pages still inherit upstream page-level composition. The official `v0.1.162` upgrade also added or expanded operational surfaces. A second phase is needed to make the visible authenticated experience materially more cohesive without mixing visual work with backend, permission, payment, or safety behavior.

The existing `coococode-ui-system-redesign` tasks 5.3 and 6.3 remain open because production has no reusable signed-in browser session. Phase two therefore starts from an authorized production-shaped user/admin baseline and an issue-to-batch acceptance matrix; source inspection and public-page screenshots are not substitutes.

## What Changes

- Record the complete authenticated route, role, mode, feature-source, and safety matrix before implementation. Use non-production synthetic accounts for candidate work; never copy a production cookie, token, credential, or sensitive record into a candidate.
- Turn same-role, same-fixture, same-feature-state screenshots into numbered visual issues. A batch may change only the files frozen for those issues and must close at least one preselected Important visual issue with measurable before/after evidence.
- Split shared work into two checkpoints: P0A covers only baseline-justified shell components; P0B covers only baseline-justified table/dialog/status components. Shared changes are not considered isolated from Audit, Prompt Audit, Batch Image, Ops, or Payment merely because those page files are untouched.
- Redesign high-frequency user self-service pages as P1: dashboard, API keys, usage, profile, available channels, and channel status.
- Redesign high-density administrator operations as P2: dashboard, users, groups, channel pricing (`/admin/channels/pricing`), accounts, and usage.
- Normalize secondary user and administrator workflows as P3 only after P0-P2: user subscriptions/redeem/affiliate, then admin subscriptions/announcements/proxies/redeem/promo/affiliates. Settings, custom pages, Channel Monitor admin, Audit Log, Risk Control, Prompt Audit, Batch Image, Ops, and all Payment surfaces stay deferred.
- Require focused tests for every modified route or shared contract, fixed responsive and breakpoint-edge browser evidence, and an independent read-only frontend review after every batch.

## Capabilities

### New Capabilities

- `authenticated-ui-quality`: Defines authenticated evidence, route batching, responsive geometry, operational density, interaction preservation, upstream-conflict limits, feature-gate isolation, and release requirements for the second-phase coococode frontend.

### Modified Capabilities

No existing capability contract is modified. This change builds on `coococode-brand-system` and keeps its API, data, authentication, authorization, billing, gateway, and deployment boundaries intact.

## Impact

- Primary code area: only the baseline-approved files under `frontend/src/components/layout`, shared presentation components, and selected P1-P3 views.
- Shared-component impact: an opt-in variant is preferred. A global shared change requires fixture/read-only regression evidence for every affected route class, including deferred high-risk consumers; destructive controls are never exercised.
- Primary tests: each modified view/component receives a focused Vitest target, followed by the existing complete frontend gate.
- Runtime: frontend bundle only. Route/meta, stores, API/request modules, payloads, permissions, feature-setting sources, backend code, database migrations, and production data are out of scope.
- Upstream compatibility: each batch freezes its file list, records the batch diff from `ecb7c2e3a`, inspects every changed file relative to official `v0.1.162`, and re-enters review before adding another upstream-owned file.
- Operations: each reviewed batch requires an isolated candidate, authorized role-specific browser evidence, an immutable CI image, backup, and the established `sub2api`-only rollback path before production replacement.
