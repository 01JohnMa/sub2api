## ADDED Requirements

### Requirement: Codex manifest through API Key upstream
The system SHALL preserve the existing direct ChatGPT manifest path for OAuth accounts. When the selected OpenAI account is an API Key account without a Codex OAuth access token, the system SHALL fetch the Codex manifest from that account's configured upstream using its existing API Key and proxy settings.

#### Scenario: Preserve OAuth manifest behavior
- **WHEN** the selected account provides a Codex OAuth access token
- **THEN** the gateway requests the ChatGPT Codex manifest using the existing OAuth path
- **THEN** API Key fallback behavior is not invoked

#### Scenario: Fetch manifest from CPA API Key upstream
- **WHEN** a Codex client requests `/v1/models` with `client_version`
- **WHEN** the selected account is an API Key account whose upstream exposes a Codex manifest
- **THEN** the gateway requests exactly one upstream `/v1/models` path with the client version
- **THEN** the response is returned as a Codex manifest containing the allowed GPT-5.6 model slugs

#### Scenario: Reject a non-manifest upstream response
- **WHEN** the API Key upstream returns an error or a JSON response without a top-level `models` array
- **THEN** the gateway returns a bounded upstream error
- **THEN** the gateway does not expose credentials or return an OpenAI list as a Codex manifest

### Requirement: Manifest model visibility follows account policy
The API Key fallback manifest SHALL expose only models allowed by the selected Sub2API account when an account model whitelist is configured. OAuth manifest passthrough behavior SHALL remain unchanged.

#### Scenario: Filter CPA manifest by account whitelist
- **WHEN** CPA returns models both inside and outside the selected account whitelist
- **THEN** the response preserves manifest objects whose `slug` is allowed
- **THEN** disallowed model objects are omitted
- **THEN** the filtered response does not reuse the upstream ETag

### Requirement: GPT-5.6 client defaults
New CC Switch and Use Key Codex configurations SHALL default to `gpt-5.6-sol` and SHALL use an OpenAI-compatible Base URL ending in exactly one `/v1`.

#### Scenario: Import OpenAI provider into CC Switch
- **WHEN** a user imports an OpenAI group into CC Switch
- **THEN** the deeplink contains `app=codex`, `model=gpt-5.6-sol`, and an endpoint ending in `/v1`
- **THEN** an existing `/v1` suffix is not duplicated

#### Scenario: Render Codex configuration files
- **WHEN** the Use Key modal renders normal or WebSocket Codex configuration
- **THEN** `model` and `review_model` are both `gpt-5.6-sol`

### Requirement: Traceable production release
The change MUST be released from a commit based on production source `d601f4a`, MUST publish an immutable custom image, and MUST retain the previous image digest for rollback.

#### Scenario: Accept the production deployment
- **WHEN** the new Sub2API image is deployed
- **THEN** Sub2API, PostgreSQL, Redis, and CPA remain healthy
- **THEN** public manifest contains Sol, Terra, and Luna
- **THEN** one minimal Sol request and one 5.5 request succeed through the existing CPA upstream

#### Scenario: Roll back a failed deployment
- **WHEN** health, manifest, Sol, or 5.5 acceptance fails
- **THEN** only Sub2API is restored to the recorded previous image digest
- **THEN** CPA, databases, credentials, and public routing remain unchanged
