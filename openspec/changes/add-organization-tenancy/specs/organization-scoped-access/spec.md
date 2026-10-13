## ADDED Requirements

### Requirement: Organization scope is derived from current server state

Organization routes SHALL derive the current organization from the authenticated User/API Key context and SHALL NOT accept a client-selected organization ID for authorization.

#### Scenario: Client attempts to select another organization

- **WHEN** a valid organization administrator sends another organization ID in a query, body, or header
- **THEN** the server ignores it and evaluates the request only against the current organization

### Requirement: Organization reads are hard-scoped

Every organization list, detail, count, pagination total, filter, and aggregate query SHALL include the server-derived organization predicate. A valid caller targeting an object in another organization SHALL receive a non-disclosing 404.

#### Scenario: Cross-organization object access

- **WHEN** organization administrator A requests a member, key, or statistic belonging to organization B
- **THEN** the system returns 404 and does not reveal whether the object exists

### Requirement: Organization overview reuses existing statistics

The organization overview SHALL expose only existing usage/error statistics filtered by organization attribution and a bounded time range. It SHALL NOT expose balances, ledger entries, provider evidence, request bodies, error details, full API Keys, or platform resources.

#### Scenario: Scoped overview

- **WHEN** organization administrator A requests the overview for a valid time range
- **THEN** the response contains only organization A's attributed statistics and approved member summary fields

### Requirement: Frontend scope is not an authorization mechanism

Frontend navigation MAY show organization pages for organization administrators, but backend route and object checks SHALL remain authoritative. Organization pages SHALL not include funding, allocation, reservation, reconciliation, or billing controls in this change.

#### Scenario: Hidden frontend control is bypassed

- **WHEN** a non-administrator calls an organization management endpoint directly
- **THEN** the backend rejects the request regardless of frontend navigation state
