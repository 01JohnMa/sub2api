## ADDED Requirements

### Requirement: Stable upstream release is traceable
The integration SHALL identify an immutable official Sub2API release tag, its commit, the coococode source commit, and their merge base before changing source.

#### Scenario: Upgrade target is selected
- **WHEN** the upgrade work begins
- **THEN** official tag `v0.1.162`, its commit, coococode commit `db290d77`, and the merge base are recorded

### Requirement: User-owned work remains isolated
The integration MUST use an isolated branch and worktree and MUST NOT overwrite uncommitted files in the main coococode worktree.

#### Scenario: Main worktree is dirty
- **WHEN** unrelated modified or untracked files exist in the main worktree
- **THEN** the upstream merge and conflict resolution occur in a separate worktree without modifying those files

### Requirement: Upstream behavior and coococode customization are preserved deliberately
Conflict resolution SHALL prefer upstream business, security, API, state, validation, and feature behavior while retaining the smallest compatible coococode brand and verified compatibility delta.

#### Scenario: Business logic and visual customization conflict
- **WHEN** an upstream release and coococode both changed the same frontend file
- **THEN** the resolution retains upstream business behavior and adapts coococode presentation to the upstream structure

#### Scenario: Existing coococode compatibility fix overlaps upstream
- **WHEN** an existing coococode fix overlaps an upstream implementation
- **THEN** tests demonstrate whether the upstream implementation supersedes the fix or the minimal remaining delta is retained

### Requirement: Integration does not introduce an independent service contract change
The integration MUST NOT independently redesign API, schema, authentication, authorization, billing, gateway routing, CPA, Nginx, credentials, or account-pool behavior.

#### Scenario: Upstream contains service-level changes
- **WHEN** `v0.1.162` changes service behavior
- **THEN** the integration adopts and verifies the tagged upstream behavior without adding unrelated local contract changes

### Requirement: Source and runtime verification gates deployment
The candidate SHALL pass relevant frontend, backend, build, authenticated browser, health, and regression checks before production replacement.

#### Scenario: Candidate source is ready for review
- **WHEN** all merge conflicts are resolved
- **THEN** frontend lint, typecheck, tests and build, backend tests, focused regression tests, OpenSpec validation, final diff inspection, and independent review complete without unresolved Critical or Important findings

#### Scenario: Authenticated behavior is checked
- **WHEN** a signed-in user and admin session are available
- **THEN** representative dashboard, table, dialog, navigation, feature-flag, light/dark, locale, and responsive behavior is verified against the pre-upgrade baseline

### Requirement: Production replacement is traceable and reversible
The release image MUST be built from the reviewed integration commit, identified by immutable digest, and deployed only after recording the running production image and rollback procedure.

#### Scenario: Candidate is deployed
- **WHEN** production replacement is authorized after all gates pass
- **THEN** only the `sub2api` service is recreated and CPA, Nginx, PostgreSQL, Redis, credentials, and routing remain unchanged

#### Scenario: Critical smoke check fails
- **WHEN** health, startup logs, public/API behavior, or authenticated smoke checks fail after replacement
- **THEN** the previous image is restored and the same checks are rerun
