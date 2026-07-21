## ADDED Requirements

### Requirement: Security-fixed dependency baseline
The release source SHALL use dependency and toolchain versions that are outside the affected ranges of all reachable high-or-higher vulnerabilities reported by the repository security workflows.

#### Scenario: Reachable backend advisory has a fixed version
- **WHEN** `govulncheck` reports a reachable vulnerability with a supported fixed Go or module version
- **THEN** the release source SHALL use the minimum compatible fixed version or newer and the backend scan SHALL pass

#### Scenario: Frontend production advisory has a fixed version
- **WHEN** the production dependency audit reports a high-or-higher advisory with a compatible patched version
- **THEN** the release source SHALL resolve that package outside the affected range and the frontend audit SHALL pass

### Requirement: Toolchain declaration consistency
The Go version declared by the module, CI verification, and release Docker builder SHALL remain aligned.

#### Scenario: Go security baseline changes
- **WHEN** the Go baseline is upgraded for a security fix
- **THEN** `backend/go.mod`, the Docker build argument, and the Security Scan version assertion SHALL identify the same Go release

### Requirement: Explicit security exception decision
The project MUST NOT silently extend or add a vulnerability exception when no compatible fixed dependency path is available.

#### Scenario: Existing exception expires without a supported fix
- **WHEN** an exception has expired and investigation finds no minimal compatible supported fix
- **THEN** implementation and production promotion SHALL stop until an owner explicitly chooses replacement, removal, or a time-bounded risk acceptance

#### Scenario: Exception is approved
- **WHEN** an owner explicitly accepts a time-bounded exception
- **THEN** the exception record SHALL identify the advisory, affected package, rationale, mitigation, accountable owner, and new expiry date

### Requirement: Green release gate before production promotion
The production service SHALL only be replaced by a commit-addressable release image after required tests, builds, and security scans pass for the same source commit.

#### Scenario: Any required gate fails or is incomplete
- **WHEN** CI, Security Scan, image build, manifest verification, or commit metadata verification fails or is incomplete
- **THEN** production SHALL remain on its current image

#### Scenario: All release gates pass
- **WHEN** the same source commit passes backend and frontend validation, Security Scan, image build, and immutable image verification
- **THEN** production MAY replace only the `sub2api` service using the verified `linux/amd64` image while retaining the previous image and configuration for rollback

### Requirement: Post-cutover verification and rollback
The production cutover SHALL include origin, public, authentication, API, and responsive page verification with an executable rollback path.

#### Scenario: Cutover verification succeeds
- **WHEN** the new container is healthy and all required smoke checks return their expected status and behavior
- **THEN** the verified image SHALL remain active and the deployment evidence SHALL be recorded

#### Scenario: Cutover verification fails
- **WHEN** health, routing, authentication, API, or critical page checks fail after replacement
- **THEN** the operator SHALL restore the previous image and backed-up configuration, recreate only `sub2api`, and repeat health checks
