## Production evidence

- Screenshot time anchor: `2026-07-31 09:41:24 +08:00`.
- The inspected database window contained HTTP-200 streaming failures recorded as generic `Upstream request failed` with `upstream_errors` empty.
- Nginx, Sub2API, CPA, PostgreSQL, and Redis were healthy during read-only inspection; the evidence did not prove the physical disconnect point.

## TDD evidence

The new focused tests initially failed to compile because `RecordOpenAIForwardFallbackError` and the cause-aware handler signature did not exist.

After implementation, the following focused suites passed in a temporary complete copy of the same repository backend:

```text
go test ./internal/service -run '^(TestRecordOpenAIForwardFallbackError_.*|TestOpenAIStreamingMissingTerminalEventReturnsIncompleteError|TestOpenAIStreamingPassthroughMissingTerminalEventReturnsIncompleteError|TestOpenAIStreamingPreambleOnlyMissingTerminalReturnsFailover)$' -count=1
go test ./internal/handler -run '^(TestOpenAIEnsureForwardErrorResponse_.*|TestOpenAIHandleStreamingAwareError_ResponsesStreaming.*|TestOpenAIRecoverResponsesPanic_AppendsResponseFailedAfterWritten)$' -count=1
go test ./internal/pkg/apicompat -run '^(TestStream_ToolCallLifecycleComplete|TestResponsesEventToAnthropicEvents_TopLevelTerminalUsage|TestResponsesEventToChatChunks_TopLevelTerminalUsage)$' -count=1
```

All three commands returned exit code 0.

The same temporary complete backend also passed:

```text
go build ./cmd/server
```

## Contract validation

```text
openspec validate fix-responses-stream-failure-observability --strict
```

Result: `Change 'fix-responses-stream-failure-observability' is valid`.

## Environment limitation

Direct tests in the root workspace could not start because `backend/go.mod` and the root Git control files are macOS FileProvider `dataless` placeholders and reads return `operation timed out`. No Git metadata was rebuilt or replaced. Formatting checks for the changed Go files returned clean.

## Review

The final equivalent patch and all new files were reviewed read-only. No Critical or Important issue remained. During review, unknown raw forwarding errors were changed to a canonical internal message instead of being persisted verbatim, plain-text `context canceled` and confirmed downstream-disconnect errors were excluded from upstream attribution, and tests were extended to assert that no synthetic upstream HTTP status is stored.

Residual risk: this fallback boundary cannot recover the upstream account ID or upstream request ID after the service layer has lost them. Those fields require a later service-level event at the exact read/terminal failure site and an authorized production deployment plus real-request correlation.
