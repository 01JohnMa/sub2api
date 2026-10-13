## ADDED Requirements

### Requirement: Platform administrators manage organization identity

The system SHALL allow only an active platform administrator to create, list, read, and update an organization. An organization SHALL contain a stable ID, normalized globally unique code, name, optional description, active/disabled status, creator, and timestamps.

#### Scenario: Create an organization

- **WHEN** an active platform administrator submits a valid unused code and name
- **THEN** the system creates one active organization and returns non-secret fields

#### Scenario: Duplicate code

- **WHEN** a platform administrator submits a normalized code already used by an organization
- **THEN** the system returns a stable conflict and creates no organization

### Requirement: Disabled organizations are not organization-admin scopes

The system SHALL reject organization-admin read and management requests when the current organization is disabled. Re-enabling an organization SHALL not alter membership or historical telemetry.

#### Scenario: Disabled organization access

- **WHEN** a member requests the organization overview after the disable transaction commits
- **THEN** the system returns a stable organization-unavailable response and exposes no scoped data

### Requirement: Existing installations remain unorganized

The migration SHALL leave existing users, API Keys, usage rows, and error rows with NULL organization attribution. It MUST NOT infer organization from Group, subscription, payment, provider account, or OAuth metadata.

#### Scenario: Upgrade existing data

- **WHEN** the migration runs on an installation with existing records
- **THEN** those records continue through the personal path and retain NULL organization attribution
