## ADDED Requirements

### Requirement: Structured fallback observability

When the OpenAI Responses handler synthesizes a fallback after forwarding fails, the system SHALL add a sanitized structured event to the existing Ops upstream-error context.

#### Scenario: Missing terminal after output

- **WHEN** an upstream Responses stream ends after client output without a terminal event
- **THEN** the handler emits a protocol-valid `response.failed` with the generic client message
- **THEN** the Ops event kind is `stream_missing_terminal`
- **THEN** the event does not invent an upstream HTTP status

#### Scenario: Stream read error after output

- **WHEN** reading the upstream stream fails after client output has started
- **THEN** the client receives the existing generic failure contract
- **THEN** the Ops event records a sanitized `stream_read_error` cause

### Requirement: Secret-safe internal detail

Internal fallback detail MUST be sanitized and bounded before being stored, and MUST NOT replace the generic client-facing message.

#### Scenario: Error contains a sensitive query value

- **WHEN** a forwarding error contains an access token or other recognized sensitive query parameter
- **THEN** the persisted event redacts the value
- **THEN** the client response does not contain the internal error text

### Requirement: Accurate attribution

The fallback recorder MUST NOT claim an upstream HTTP status when none is known, and MUST NOT record explicit client cancellation as a provider failure.

#### Scenario: Client cancels the request

- **WHEN** the forwarding error wraps `context.Canceled`
- **THEN** no fallback upstream event is appended

### Requirement: Event deduplication

The system SHALL suppress an identical fallback event while preserving distinct prior failover or HTTP error events.

#### Scenario: The same fallback is recorded twice

- **WHEN** the same kind and sanitized message are recorded more than once for one request
- **THEN** only one matching event remains in `upstream_errors`
