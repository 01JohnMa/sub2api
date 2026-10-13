## ADDED Requirements

### Requirement: A user belongs to at most one organization

The system SHALL represent at most one current organization and one organization role on a User. A user without an organization SHALL retain the existing personal behavior. This change SHALL NOT introduce multi-organization switching or invitation workflows.

#### Scenario: Join an organization

- **WHEN** a platform administrator adds an unorganized, non-deleted user to an active organization
- **THEN** the user receives exactly one organization membership and a valid organization role

#### Scenario: Already-organized user

- **WHEN** an administrator tries to add a user who already belongs to an organization
- **THEN** the system returns a stable conflict without changing either organization

### Requirement: Organization roles do not grant platform authority

Organization administrator/member roles SHALL remain separate from the existing platform administrator role. Organization administrators SHALL not gain access to global admin resources.

#### Scenario: Organization member calls a platform route

- **WHEN** an organization administrator or member calls an existing platform-admin route
- **THEN** the request is denied by the existing platform authorization boundary

### Requirement: Membership changes affect new request attribution only

Membership removal SHALL clear the current organization relationship after authorization and locking checks. It SHALL NOT rewrite organization attribution already stored on usage or error records.

#### Scenario: Remove a member with historical usage

- **WHEN** a member is removed after making organization-attributed requests
- **THEN** future requests use the personal path while previous records remain attributed to the original organization
