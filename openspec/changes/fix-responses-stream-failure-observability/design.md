## Context

The direct Responses relay can return a plain error after downstream output has started. The handler then appends a generic `response.failed`, while the HTTP status remains 200. Today that fallback does not append an `OpsUpstreamErrorEvent`, so the monitoring middleware persists either an empty generic record or no actionable upstream detail.

## Goals / Non-Goals

**Goals:**

- Ensure every synthesized Responses fallback has a structured, sanitized Ops event.
- Preserve the real error category when it is available to the handler.
- Avoid duplicate events when the service layer already recorded the same failure.
- Avoid attributing explicit client cancellation as an upstream failure.

**Non-Goals:**

- Retry or replay a response after partial client output.
- Expose internal errors to clients.
- Add columns, indexes, migrations, or new production logging infrastructure.
- Modify CPA or claim the physical disconnect point is known.

## Decisions

### 1. Record at the handler fallback boundary

The handler fallback is the final common boundary for plain errors that were not already communicated by the service layer. Recording here closes the observability gap without changing each streaming implementation in the first patch. Service-specific terminal and byte counters can be added later when an end-to-end trace is available.

### 2. Keep the client message generic

The client continues to receive `Upstream request failed`. The internal event stores a sanitized and bounded cause so credentials, query tokens, or oversized error strings are not exposed.

### 3. Do not synthesize an upstream HTTP status

Post-output EOF and read failures often have no upstream status. Their event keeps `upstream_status_code` unset instead of incorrectly claiming 502. The existing request-level status remains HTTP 200 because headers were already committed.

### 4. Classify and deduplicate conservatively

Known causes use stable kinds such as `stream_missing_terminal`, `stream_read_error`, and `stream_deadline_exceeded`. Re-recording the same kind/message pair is suppressed, while a distinct final failure remains visible after earlier failover events. Explicit context cancellation and confirmed downstream disconnect errors are not recorded as provider failures.

## Validation

- Focused service tests for classification, sanitization, deduplication, and cancellation.
- Focused handler test for HTTP-200 Responses fallback plus structured Ops context.
- Existing handler/service/apicompat stream tests.
- OpenSpec strict validation and read-only review before delivery.
