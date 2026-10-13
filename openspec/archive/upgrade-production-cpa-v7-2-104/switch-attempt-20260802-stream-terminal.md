# CPA v7.2.104 Switch Attempt - Stream Terminal Gate - 2026-08-02

## Result

The third controlled production attempt did not complete the upgrade. The candidate passed the process gate, live account-pool gate, Responses gate, Chat gate, and non-stream tools gate. The stream-continuation case then failed at the unique-terminal contract, and the still-armed controller restored the fixed `v7.2.82` binary and verified the rollback gate.

No config, systemd unit/drop-in, OAuth directory, account weight, routing mode, session affinity, plugin setting, Sub2API image, Nginx config, or database data was changed.

## Evidence

- The candidate service stopped at `19:48:10` UTC and started at `19:48:11` UTC. Rollback stopped it at `19:50:14` UTC and started the verified `v7.2.82` at the same second.
- The controller collected a fresh 24-hour conditional-usage scan with zero WebSocket, Live/sideband/WebRTC, image, and video rows, then collected a fresh old-version baseline before claiming the switch window.
- The candidate process gate passed with active service, listener, candidate hash, and exact `v7.2.104` version.
- `case_live`, `case_responses`, `case_chat`, and `case_tools` passed.
- The validator stopped at `stream_continuation_contract_failure`; the controller-triggered rollback passed active/listener/hash/exact-version verification for `v7.2.82`.
- After rollback, the same redacted stream-continuation case was replayed three times on `v7.2.82`: two clean passes and one first-stream `invalid_terminal_count`. A separate replay produced one clean first stream followed by one continuation `invalid_terminal_count`, then the next replay passed. No request body, raw response ID, raw call ID, credential, or response text was retained.

## Diagnosis

The available evidence establishes an intermittent malformed SSE terminal sequence on the existing production path, but does not establish a `v7.2.104`-specific regression. The original unique-terminal gate remains strict on the successful attempt. The validator contract now classifies only `dual_terminal`, `invalid_terminal_count`, and `invalid_terminal_order` as one-use transport-transient failures; a complete retry must produce exactly one terminal event with that event last. A second malformed result remains blocking and triggers the armed rollback.

## Stop Condition

The new retry classification requires a fresh manifest/self-hash, verify-only, OpenSpec strict validation, and independent read-only Review before another production authorization. The failed authorization is not reused.
