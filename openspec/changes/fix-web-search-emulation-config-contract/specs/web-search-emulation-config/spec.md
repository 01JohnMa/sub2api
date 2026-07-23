## ADDED Requirements

### Requirement: Absent Web Search configuration is a disabled state

The system SHALL treat an absent optional `web_search_emulation_config` setting as a
valid disabled Web Search emulation configuration rather than as a missing resource.

#### Scenario: Service reads an absent setting

- **WHEN** the settings repository returns `ErrSettingNotFound` for
  `web_search_emulation_config`
- **THEN** the service SHALL return no error
- **AND** the returned configuration SHALL have `enabled` set to `false`
- **AND** the returned `providers` collection SHALL be a non-nil empty collection

#### Scenario: Admin endpoint reads an absent setting

- **WHEN** an authenticated administrator requests
  `GET /api/v1/admin/settings/web-search-emulation`
- **AND** the optional setting does not exist
- **THEN** the endpoint SHALL return HTTP 200 using the existing success envelope
- **AND** the response data SHALL contain `enabled: false`
- **AND** the response data SHALL contain `providers: []`

### Requirement: Storage failures remain observable

The system MUST distinguish an absent optional setting from an unexpected settings
repository failure.

#### Scenario: Repository returns a non-not-found error

- **WHEN** the settings repository returns a database, timeout, or other error that
  does not match `ErrSettingNotFound`
- **THEN** the service SHALL return the error through the existing error path
- **AND** reads served from the short-lived error cache SHALL continue to return that
  error
- **AND** the system MUST NOT represent the failure as a successful disabled
  configuration

### Requirement: Configured behavior remains unchanged

The system SHALL preserve the existing behavior for a valid persisted Web Search
emulation configuration.

#### Scenario: Valid configuration exists

- **WHEN** a valid `web_search_emulation_config` value exists
- **THEN** the service SHALL parse and return that configuration
- **AND** the admin response SHALL preserve its existing provider API-key field
  behavior
- **AND** the admin response SHALL continue to populate provider usage data

#### Scenario: Configuration is saved through the supported API

- **WHEN** an administrator saves a valid Web Search emulation configuration
- **THEN** the system SHALL continue to persist and hot-reload it using the existing
  behavior

### Requirement: Reading an absent setting has no side effects

The system MUST NOT create or enable Web Search emulation merely because the optional
setting was read.

#### Scenario: Missing configuration is read

- **WHEN** the absent setting is resolved to the disabled default
- **THEN** the system MUST NOT write a new setting
- **AND** the runtime Web Search manager SHALL remain disabled
