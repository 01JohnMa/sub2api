## ADDED Requirements

### Requirement: Phase two starts from authenticated evidence

The implementation SHALL record safe signed-in user and administrator evidence, a complete authenticated route matrix, and numbered visible issues before changing shared authenticated UI or route pages.

#### Scenario: Authenticated baseline is available

- **WHEN** phase-two implementation begins
- **THEN** authorized production-shaped user and admin sessions have recorded role, route, mode/feature state, fixed-width and breakpoint-edge geometry, console/API results, and safe interaction observations without exposing credentials or sensitive records

#### Scenario: No safe login session is available

- **WHEN** either role lacks an authorized safe session
- **THEN** P0 remains pending and the implementation does not create production accounts, retrieve credentials, copy production cookies/tokens, or use public pages as substitute evidence

### Requirement: Every implementation batch closes a preselected visible issue

Every batch SHALL map stable issue IDs to exact routes, widths, files, and observable acceptance before editing and SHALL compare the same role, fixture, feature state, and viewport afterward.

#### Scenario: A batch has measurable scope

- **WHEN** implementation starts for a batch
- **THEN** its frozen evidence matrix contains at least one preselected Important visual issue and every proposed file is traced to an accepted issue

#### Scenario: A batch has no Important visible issue

- **WHEN** the baseline does not identify an Important issue for a proposed batch
- **THEN** that batch is a no-op until a separately reviewed measurable objective is accepted

### Requirement: Shared authenticated components preserve behavior and account for all consumers

The frontend SHALL prefer opt-in presentation variants and SHALL treat a global shell, table, dialog, card, or status change as affecting every consumer, including deferred high-risk surfaces.

#### Scenario: Responsive shell crosses its breakpoint

- **WHEN** the app is checked at 1023 and 1024 px
- **THEN** the existing drawer/desktop switch, overlay, header, content offset, custom menu, feature visibility, theme, route guards, scroll restoration, and mobile-close behavior remain correct for both roles

#### Scenario: DataTable crosses its breakpoint

- **WHEN** a table is checked at 767 and 768 px
- **THEN** the existing card/table semantic switch, selection, multi-page selection, server sorting, virtualization, pagination, and row actions remain functional

#### Scenario: A dialog opens and closes

- **WHEN** a shared dialog is opened, confirmed, cancelled, closed with Escape, or dismissed in focused tests
- **THEN** focus restoration, body scroll lock, emitted events, destructive confirmation, and width containment remain correct

#### Scenario: A global shared change reaches a deferred surface

- **WHEN** Audit Log, Prompt Audit, Batch Image, Ops, or Payment consumes the changed component
- **THEN** the batch records focused fixture tests and read-only or feature-off visual evidence without executing save, clear, probe, delete, payment, refund, or callback actions

### Requirement: User self-service pages are responsive and cohesive

The frontend SHALL give `/dashboard`, `/keys`, `/usage`, `/profile`, `/available-channels`, and `/monitor` a consistent hierarchy and responsive action layout without changing business requests, account behavior, feature sources, or direct-route semantics.

#### Scenario: User works at phone width

- **WHEN** a signed-in user opens a modified P1 route at 390 x 844
- **THEN** the document has no global horizontal overflow and primary actions, filters, pagination, cards, and dialogs remain reachable

#### Scenario: User works at tablet or desktop width

- **WHEN** a signed-in user opens a modified P1 route at 768 x 900 or 1440 x 900
- **THEN** filters, tables/charts, detail controls, and status information remain scannable without changing copy, edit, delete, OAuth, TOTP, refresh, feature-menu, or request behavior

### Requirement: Administrator operations retain density

The frontend SHALL improve only `/admin/dashboard`, `/admin/users`, `/admin/groups`, `/admin/channels/pricing`, `/admin/accounts`, and `/admin/usage` in P2 while preserving their bulk operations, server behavior, payloads, permissions, and confirmations.

#### Scenario: Administrator uses dense tables

- **WHEN** an administrator opens a modified P2 table route at 1440 x 900
- **THEN** the filter/action bar and table header each have bounding boxes with `top >= 0` and `bottom <= 900`, rows/actions remain compact, and the document has no page-level horizontal overflow

#### Scenario: Administrator uses a narrow viewport

- **WHEN** an administrator opens a modified P2 route at 390 or 768 px
- **THEN** bulk actions, column controls, filters, dialogs, and row actions remain reachable while any horizontal scrolling is contained to an explicit table wrapper

### Requirement: Responsive geometry is measurable

Every modified route SHALL pass fixed 390 x 844, 768 x 900, and 1440 x 900 checks, while changed shared semantics also pass their 767/768 or 1023/1024 edge pair.

#### Scenario: Document and dialog are measured

- **WHEN** a modified route is checked at 390 x 844
- **THEN** `document.documentElement.scrollWidth === clientWidth`, and an opened dialog has `left >= 16` and `right <= innerWidth - 16` with confirm and cancel reachable

#### Scenario: A table legitimately overflows

- **WHEN** table content is wider than its 768 px viewport
- **THEN** only the named table wrapper has `scrollWidth > clientWidth`, its ancestors through the document do not, and the document width remains equal to the viewport client width

### Requirement: Feature and permission contracts do not drift

Phase-two visual work SHALL preserve role guards, standard/simple/backend behavior, public versus admin feature sources, custom-menu settings, and existing direct-route behavior.

#### Scenario: Optional feature is disabled or menu-hidden

- **WHEN** payment, risk control, affiliate, channel monitor, available channels, Ops, Batch Image, or an admin feature is disabled, unavailable, or hidden in simple mode
- **THEN** menu visibility and route redirection remain governed by their existing distinct setting, account-eligibility, and router sources

#### Scenario: User lacks administrator role

- **WHEN** a regular user attempts an administrator route
- **THEN** the existing redirect and authorization behavior remains unchanged by the visual implementation

### Requirement: High-risk feature surfaces remain separate

Settings, custom pages, Channel Monitor admin, Audit Log, Risk Control, Prompt Audit, Batch Image, Ops, and Payment SHALL NOT enter P1-P3 until their own feature state, destructive-action boundary, safe data, exact files, and dedicated tests have been reviewed.

#### Scenario: A general page batch reaches a high-risk page file

- **WHEN** implementation would alter a deferred surface's page, script, store, API/request, payload, permission, payment, probe, deletion, or access semantics
- **THEN** the alteration stops and is deferred to a separate OpenSpec change

### Requirement: Upstream-compatible scope is frozen

Every batch SHALL freeze files from the `ecb7c2e3a` baseline, inspect touched files relative to official `v0.1.162`, prefer wrappers/classes, and prohibit unreviewed business-layer edits.

#### Scenario: Implementation expands beyond frozen files

- **WHEN** a new file, view script, router, store, API/request module, or payload path would be changed
- **THEN** implementation stops until the issue/file matrix and independent plan review are renewed

### Requirement: Every batch has reproducible acceptance and release evidence

Each implemented batch SHALL record focused tests for every modified route/component, the complete frontend commands, isolated candidate recipe, role-appropriate browser matrix, immutable image, review, backup, smoke, and rollback evidence.

#### Scenario: Batch is proposed for review

- **WHEN** a batch completes implementation
- **THEN** its evidence contains exact focused test paths, `pnpm lint:check`, `pnpm typecheck`, `pnpm test:run`, `pnpm build`, dependency exception check, strict OpenSpec validation, candidate health `{"status":"ok"}`, and zero newly introduced or unowned console/API errors

#### Scenario: Batch is proposed for release

- **WHEN** a reviewed batch is ready for promotion
- **THEN** an exact-commit `linux/amd64` image/digest, current production image, backup path, `sub2api`-only replacement, health/log/API/browser smoke, and executable rollback plus post-rollback health check are recorded

#### Scenario: Review finds a blocking issue

- **WHEN** an independent review reports a Critical or Important finding
- **THEN** the batch is not published or deployed until the finding is fixed and affected verification is rerun
