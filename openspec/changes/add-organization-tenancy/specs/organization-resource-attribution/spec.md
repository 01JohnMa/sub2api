## ADDED Requirements

### Requirement: Request organization attribution is server-derived

After existing API Key/User authentication, the system SHALL resolve the current organization from persisted server state. Client headers, query parameters, body fields, and frontend route parameters MUST NOT select the organization used for authorization or telemetry.

#### Scenario: Trusted organization context

- **WHEN** an authenticated API Key owner belongs to active organization A
- **THEN** the request context contains organization A and no client-supplied organization value can override it

#### Scenario: No organization

- **WHEN** an authenticated API Key owner has NULL organization attribution
- **THEN** the request continues through the existing personal path with NULL organization context

### Requirement: Existing request execution is reused

Organization attribution SHALL be an input to the existing gateway execution, routing, and usage/error writer paths. The system MUST NOT require an organization-specific reservation, ledger, final-evidence, or reconciliation state machine for this change.

#### Scenario: Organization request uses existing path

- **WHEN** a request has a trusted active organization context
- **THEN** it uses the same upstream adapter, retry semantics, and personal metering logic as the existing request path while carrying the organization context to telemetry

### Requirement: Telemetry stores write-time organization attribution

Successful usage and attributable error records SHALL store the organization ID captured when the request was accepted. Pre-authentication failures and records without a trusted organization SHALL keep NULL attribution.

#### Scenario: Historical attribution survives membership change

- **WHEN** a request is accepted under organization A and the user is later removed
- **THEN** that request's usage/error record remains attributed to A and is not reclassified as personal or another organization

### Requirement: Organization attribution does not change personal billing

This change SHALL NOT introduce organization funds or alter personal balance, personal subscription, API Key quota, Group routing, or provider account accounting.

#### Scenario: Organization context with existing billing

- **WHEN** an organization-attributed request is processed
- **THEN** its existing personal metering and routing contract remains unchanged; only the reporting attribution is additional
