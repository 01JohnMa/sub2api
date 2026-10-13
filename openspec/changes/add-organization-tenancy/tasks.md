# Tasks: organization reporting dimension

## 1. Identity And Membership

- [x] 1.1 Confirm the existing Organization/User fields and keep Organization independent from Group and provider metadata.
- [x] 1.2 Add or reuse active/disabled organization identity operations; do not add archive-with-funds semantics in this change.
- [x] 1.3 Reuse the existing single-organization membership and role checks for platform administrators and organization administrators.
- [x] 1.4 Add two-organization tests for join, remove, role checks, foreign-object 404, and no cross-organization access.

## 2. Trusted Request Attribution

- [x] 2.1 Resolve the current organization from server-side API Key/User state after existing authentication; ignore client-supplied organization values.
- [x] 2.2 Keep personal behavior unchanged when the resolved organization is NULL.
- [x] 2.3 Define the disabled-organization read/access response without introducing organization funding or billing behavior.
- [ ] 2.4 Test membership change versus concurrent request attribution: old accepted requests retain their captured organization, new requests use committed state.

## 3. Reuse Existing Telemetry And Reports

- [x] 3.1 Add nullable request-time `organization_id` attribution to existing successful usage and attributable error records/writers.
- [x] 3.2 Propagate the trusted organization context through existing gateway handlers and usage/error writer calls; do not add a second execution state machine.
- [x] 3.3 Add organization predicates to existing list, aggregate, pagination-total, and date-range queries.
- [x] 3.4 Preserve NULL attribution for pre-authentication failures and all historical rows created before organization assignment.
- [x] 3.5 Add report tests proving organization A cannot see B, historical rows do not change after membership removal, and personal reports remain unchanged.

## 4. Minimal Organization Read API/UI

- [x] 4.1 Expose current organization identity, member summary, and read-only usage/error overview from server-derived scope.
- [x] 4.2 Keep organization responses free of balances, ledger entries, provider evidence, request bodies, error details, full API Keys, and platform resources.
- [x] 4.3 Add frontend organization summary/overview only; do not add funding, allocation, reservation, reconciliation, or billing pages.

## 5. Verification And Rollout

- [x] 5.1 Run targeted backend schema, auth, telemetry, reporting, handler, and two-organization integration tests.
- [ ] 5.2 Run frontend lint, typecheck, unit tests, and build using existing repository scripts when frontend files are changed.
- [x] 5.3 Run `openspec status --change add-organization-tenancy --json` and strict validation.
- [x] 5.4 Review the final diff and verify no organization allocation, reservation, ledger, final-evidence, pending-reconciliation, or independent billing requirement remains in this change.
- [ ] 5.5 Roll out nullable schema and compatible code first; enable organization statistics only after personal-path regression checks pass.

## Deferred To A Separate Change

- organization funding and credit;
- member allocation, reclaim, and balance invariants;
- organization Key admission based on allocation;
- reservation, attempt/provider evidence, final execution evidence, outbox, and pending reconciliation;
- organization settlement, ledger, archive freeze, and independent quota.
