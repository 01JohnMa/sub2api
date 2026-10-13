## Status

The first authorized attempt ran on `2026-07-31` Asia/Shanghai, failed closed at `live_contract_failure`, and automatically restored verified `v7.2.82`. That authorization has been consumed. See `switch-attempt-20260731.md` for the redacted timeline, impact, recovery, and OAuth snapshot remediation.

A second explicit switch authorization was received on `2026-08-01`. The mandatory pre-switch Review first found transport/overload classification, dual-terminal SSE handling, and stable error-signature gaps; those were remediated before execution. The subsequent controlled attempt started verified `v7.2.104`, passed process, live, Responses, and Chat gates, then failed closed at `tools_contract_failure` and automatically restored verified `v7.2.82`. See `switch-attempt-20260802.md` for the redacted timeline and diagnosis.

The second failure was a validator contract defect, not evidence of a `v7.2.104` regression: the public Sub2API HTTP path already documented tool-result `previous_response_id` as a 400 contract limited to Responses WebSocket v2, while the validator incorrectly required an HTTP SSE continuation to return 200. A third attempt then passed process/live/Responses/Chat/tools and rolled back at an intermittent malformed SSE terminal that was also reproduced on `v7.2.82`; see `switch-attempt-20260802-stream-terminal.md`. A fourth attempt later rolled back at `live_consecutive_or_changed_failure`; the same non-zero global `failed` delta was reproduced on unchanged `v7.2.82`, proving that this cumulative concurrent counter was not attributable to the controlled case. `switch-attempt-20260802-live-counter.md` records the diagnosis and the replacement hard-auth/telemetry contract. The fourth authorization was consumed. The rebuilt tests, manifest/self-hash, Linux snapshot test, real old-version canary, and OpenSpec strict validation pass; a fresh explicit authorization and switch-time baseline/preflight remain required before another mutation, and any drift blocks execution. The fixed rollback script remains the recovery entry point only for a matching active owner or an explicitly reviewed recovery action.

On `2026-08-02` Asia/Shanghai, the user explicitly removed the post-switch 24-hour observation requirement and confirmed this controlled execution. Success now requires immediate validation plus a 15-minute observation, extended to 30 minutes when the public sample is below 20. The existing read-only 24-hour conditional-usage scan remains a pre-switch protocol gate. The fixed `v7.2.82` backup remains retained after success and is not cleaned up without separate approval.

The third post-remediation attempt then completed that contract successfully: production is now verified `v7.2.104`, all immediate/final business and privacy gates plus the 15-minute observation passed, owner was removed, and automatic rollback was disarmed only after final verification. See `switch-success-20260802-trace-gate.md`. No further candidate switch is pending; any future binary or routing change requires a new explicit authorization and a new preflight.

## Fixed Production Inputs

- Formal binary: `/opt/cpa/bin/cli-proxy-api`.
- Config: `/opt/cpa/config.yaml`; SHA-256 `73a8df5727b08c19c1e83e694ad9420d3135615c13ba79322719e188befbe10a`.
- Unit: `/etc/systemd/system/cpa.service`; SHA-256 `9190014d8a8ab0331b6f188e4c98b0dfdc6049c714c145f531a888554c7b1111`; `DropInPaths` is empty.
- Coordination: `/run/cpa-v104-switch/` is `root:root 0700`; `lock` is a regular `root:root 0600` file; `owner` is absent while no controller owns a switch window.
- Auth directory: `/opt/cpa/auths`.
- Candidate: `/opt/cpa/staging/v7.2.104/extracted/cli-proxy-api`; SHA-256 `6355d7424394f22293f9d9c8cb3b9ca0073734dc50e8b740bb2af5cea98aaf64`.
- Rollback: `/opt/cpa/backups/cpa-v7.2.82-20260729T111659Z/cli-proxy-api`; SHA-256 `e2ea31a127240f213d046567721839d725eeb4c136225eef139c6a46aa464b17`.
- Candidate version line: `CLIProxyAPI Version: 7.2.104, Commit: c9417c8a, BuiltAt: 2026-07-28T08:36:09Z`.
- Rollback version line: `CLIProxyAPI Version: 7.2.82, Commit: f583414f, BuiltAt: 2026-07-17T08:17:41Z`.

The verified binaries print flag usage, then the exact version line, and exit with status `2` for `-version`. Deadline-aware helpers accept only exit status `2` and the exact expected line.

## Reviewed Artifacts

- Controller SHA-256: `c2a251e52871c4d9dc4970481f0062a5d7beb287f90948ca96f1f1bacca93b43`.
- Controller normalized self-check SHA-256: `f2139bafaf148a2b129b61d03a07d630a7e3c15324ca46180a25d0fdcd0bb38f`.
- Manifest SHA-256: `be9e24f75f965a377ed8791c4f2f0292182120f3fe9b76fb65527e83ad859e6f`.
- Manifest-fixed switch SHA-256: `493435d4e84451a9994d2a5444ccc35e3fc820d7b1283108ae91ebafd0f2af47`.
- Manifest-fixed rollback SHA-256: `9b61999a985937351f4088936c0280b1809f1ded99a61ca62419d1606dc153bc`.
- Manifest-fixed validator SHA-256: `22911af70f2efb5fb4291440ade8e024863a9fef0a17da3428110fb794d91ba4`.
- Manifest-fixed validator runner SHA-256: `bef4498382e6dda284208894aebe6e7de9ad5318475eff31bcb4ad3ea737ea7d`.
- Manifest-fixed live case / Responses path SHA-256: `5ceb2d9bc62581d379ecad74611351d8f573a6939206e8a75b2712ed7e007f03` / `0ffada572c758be0208dcb1326ca6057a7350dd25dbe91e2f9161055e491de36`.
- Observation collector/comparator SHA-256: `2cfff6242701b59ce91179de4089359689543c738634606c3918be9b428f6e71` / `abdeca86ec836320142495d89f1f2926d30edf4eb5e0cc82cdc19f8d503b6a1e`.
- Stream continuation/tools/long-history SHA-256: `720328fb5bd8368eff23ba08a8b22e906f8a37676c2cd4369ebe0b8faeda5586` / `76ee8f691efc7a527f3c35e9befcd0178264fa84851f5f5c34ae682753b97118` / `25b28c269697d645e0f6b87312bcd65bf2b3a9f9ac73dbf4f17d9a7e26c60079`.
- Retry/terminal/auth-counter, observation comparison, tools contract, artifact snapshot, log fail-closed, and Trace coverage tests SHA-256: `6b67055fa9692402956e39a2f08cde24b9a72ba32a5ec5ee158cc5d5c9c26616` / `258d386f83d92a14d6a9464c0c798ac51e2e9e84b89b9c3acd747b5aad90f455` / `e9ef23337634191e162980b65997b4603666da0a03fca5503840a64b7b504054` / `01bf1ef6080bc09f6beb59a2a5c4d52cae1cc670fff0529baae0509d70831854` / `23e6a09fa632b9449e0bd8cb1d026f04172ccd69ab7afe78ad9ff7ab35cdc8b8` / `442cd06af0684762b771489176867bfa0263daa9b02ea658da7de93d31e62d04`.
- Conditional-usage remote gate SHA-256: `c587f7f5d5e1f6a7b286dd2598b668ea2d35c4fcf321cd6412304deb9bc92e12`.
- Log validator SHA-256: `7635712140978539f5e926e98a6be66b50fdbb07b2df8482909cc413e0535c34`.
- Manifest-fixed owner claim/release SHA-256: `4d4b35f9078cc30610ba016904aad35616f7e4890494d18df3c0d6579cd0f9cc` / `e13986434bd7dacd5a78c7a10ba2e194f5808a16e71473b9257ddfb581e44d6d`.
- `scripts/controller.sh`: local fail-closed orchestrator; remains armed through immediate and short observation gates.
- `scripts/claim-owner-remote.sh` and `scripts/release-owner-remote.sh`: create and release the per-controller remote owner token under the shared root-only lock.
- `scripts/switch-process.sh`: remote preflight and candidate transition.
- `scripts/rollback-process.sh`: independent idempotent remote rollback.
- `scripts/collect-pre-switch-baseline.sh`: collects the old-version 15- and 30-minute comparison windows.
- `scripts/validate-immediate.sh`: concrete immediate and short-observation validator.
- `scripts/validate-remote.sh`: process/config/auth-file invariant checks.
- `scripts/observe-remote.sh`: endpoint/status sample and P95/TTFT snapshots.
- `scripts/check-conditional-usage-remote.sh`: read-only 24-hour WebSocket/Live/sideband/WebRTC/image/video usage gate.
- `scripts/billing-query-remote.sh`: redacted usage/dedup query.
- `scripts/capture-log-offsets-remote.sh`, `scripts/validate-log-remote.sh`, and `scripts/remove-log-snapshot-remote.sh`: root-only append-offset snapshot, all-log appended metadata/permission check, and idempotent cleanup.
- `scripts/log-metrics-remote.sh`: fixed remote log metric collector.
- `scripts/cases/`: direct CPA, Sub2API localhost, public Responses/Chat, production-used models, tools, stream continuation, item ID, billing, and log-rate cases.
- `scripts/tests/`: manifest-fixed retry state-machine and Trace parser/coverage tests plus a Linux `/tmp` OAuth snapshot positive/negative driver; tests never operate on the production auth directory or service.
- `scripts/tests/test-tools-followup-contract.sh` and `scripts/tests/test-artifact-snapshot.sh`: explicit replay/namespace gate and immutable local artifact-snapshot tests.

No script contains a literal API key, management key, OAuth token, cookie, or request ID. Secrets are read from the existing protected local or remote locations at runtime and are never printed.

## Revalidation Before Stop

1. Confirm explicit user approval and a low-traffic window.
2. Confirm no Sub2API/Nginx deployment, config update, OAuth login, or device flow is active.
3. Confirm official latest still resolves to the fixed approved target or record a newer release without changing the target.
4. Verify production still reports active `v7.2.82`, the formal/config/unit/candidate/rollback hashes match, `ExecStart` and `/proc/$MainPID/exe` resolve to the formal path, `DropInPaths` remains empty, `8317/8080` listen, and Sub2API's immutable image ID is unchanged.
5. Verify current routing is `round-robin`, `session-affinity=false`, plugins/GitTokenStore/legacy sync are absent, and parsed `auth-dir` is exactly `/opt/cpa/auths`.
6. Run `scripts/check-conditional-usage-remote.sh 24`; if any WebSocket mode, Live/sideband/WebRTC endpoint or user-agent, image, or video row is present, stop and add a real protocol case before switching.
7. Verify the script manifest and controller hashes and run `bash -n` on every script. The controller then freezes the verified script directory into a read-only local artifact snapshot before collecting the switch-time baseline.
8. If any input differs, stop and re-review. Do not repair or switch opportunistically.

## Controller Sequence

The authorized entry point is a reviewed file invocation with the approved SSH target supplied through `CPA_REMOTE_HOST`. The controller performs these steps:

1. Verify the fixed manifest SHA-256, then run `sha256sum -c` for every switch, rollback, validator, observer, and case script.
2. Collect 15- and 30-minute old-version endpoint/status and P95/TTFT baselines before arming the switch, after the 24-hour conditional-usage gate passes.
3. Generate a unique local run ID, set `claim_attempted=1`, and under the shared root-only `flock` verify active service, formal executable path, listener, fixed `v7.2.82` hash, and exact version before creating `/run/cpa-v104-switch/owner`. If production is already another version, claim exits before owner creation and the controller's unconfirmed-claim rollback policy performs no mutation.
4. Record successful claim as `owner_confirmed=1` and keep the owner token present through transition, immediate validation, and the 15/30-minute observation. A second controller receives owner-mismatch status and cannot switch, release, or roll back the first controller's window. An ambiguous claim rolls back only when the same owner is observed; owner absence before claim confirmation is a no-op.
5. Bound SSH connection plus the remote transition with a 120-second local wrapper. The remote script separately bounds read-only preflight to 30 seconds.
6. Switch and rollback verify the same owner token after acquiring `/run/cpa-v104-switch/lock`. An independent rollback waits up to 65 seconds for an in-flight HUP rollback to release the lock. The local emergency rollback wrapper is 150 seconds so it can cover SSH connection, lock wait, and a fresh 60-second recovery without concurrent stop/install/start.
7. At first service stop, create a 30-second candidate deadline and a 60-second recovery deadline. All stop/start, copy/install, hash, version, state, PID, executable-path, and listener operations use the applicable absolute deadline.
8. Create a unique, non-existing `root:root 0700` OAuth snapshot outer directory after the old service is stopped, then copy OAuth data only into its `auth/` child. The outer directory remains non-traversable if copy or hardening fails. Under a non-foreground process-group deadline, reject symlink/special entries with explicit `find` error propagation, recursively force and verify directories `0700` and files `0600`, generate a recursive manifest for every regular data file, and verify it without printing names, digests, or content. Normal rollback does not restore the snapshot.
9. Install through a same-filesystem temporary file, atomically rename, start, then verify active, `8317`, candidate hash, exact version line, `ExecStart`, unit/drop-in digest, `/proc/$MainPID/exe`, and remaining deadline.
10. Run the concrete validator from the frozen read-only artifact snapshot. The controller remains armed while it performs immediate cases and the 15-minute observation; fewer than 20 requests extends the observation to 30 minutes.
11. Set `release_attempted=1`, release only the matching owner token, and disarm rollback only after release returns confirmed success. Owner absence during release is ambiguous and forces conservative rollback; different owner returns a no-mutation refusal.

`CPA_VERIFY_ONLY=1` stops after controller self-check and manifest verification; it is used during preparation to prove artifact integrity without collecting a production baseline or entering the switch path.

## Systemd Deadline Policy

Production currently reports `TimeoutStartUSec=1min 30s`, `TimeoutStopUSec=1min 30s`, `KillMode=control-group`, and `SendSIGKILL=yes`. Those unit defaults exceed the upgrade's 60-second recovery contract.

The reviewed scripts therefore use `systemctl ... --no-block` and poll `ActiveState`, `MainPID`, and `8317` against an absolute deadline. If a stop has not completed within five seconds, they issue `systemctl kill --kill-who=all --signal=KILL cpa.service`, then verify PID exit and listener removal before replacing a binary. This forced termination policy is part of the switch action that requires the user's explicit confirmation; it is not executed during preparation.

If the kernel, systemd, storage, or host is unresponsive enough that the bounded rollback cannot verify active/listener/hash/version before its deadline, the script reports a redacted fatal condition and does not claim recovery success. The independently runnable rollback file remains the fixed recovery entry point from a second shell.

## Immediate Validator

The fail-closed validator covers:

- Process/config: active service, PID, restart count, exact binary/config hashes, exact version, `8317/8080`, auth-dir, round-robin, no affinity, no plugin/GitTokenStore/legacy sync drift.
- Account pool: the management endpoint must return HTTP 200 both before and after the controlled requests; both snapshots must contain the same three unique, non-empty anonymous Codex identity keys, with no disabled/unavailable/runtime-only entry or hard-auth `status_message` match. The global `success` and `failed` deltas are telemetry only: each must be a three-element vector of non-negative integers with no counter regression, but its magnitude is not a pass/fail or retry condition. The per-auth `/auth-files/models?name=...` endpoint must confirm that the selected model is present for all three expected credentials. In candidate `v7.2.104` mode, dedicated non-stream probes must cover all three expected `.auth_index` values through the final successful `X-CPA-TRACE-ID` response header within at most 12 attempts and an absolute 30-second deadline; the header must have exactly one value in the final response block and match `^[0-9]{14}-[0-9a-f]{16}-[0-9a-f]{8}$`. Missing, duplicate, unknown, malformed, over-budget, or incomplete coverage is blocking. Aggregate status may be `active` or `error` because CLIProxyAPI retains model-scoped errors at auth level; `pending`, `refreshing`, `unknown`, or `disabled` remains blocking, and `error` is accepted only while the post-request snapshot still has `unavailable=false`. The v7.2.82 legacy rollback path does not require the candidate Trace header. Actual selected-model Responses/Chat requests remain the health evidence.
- Paths: direct CPA, tunneled Sub2API localhost, and public Responses/Chat.
- Production models: `codex-auto-review`, `gpt-5.4`, `gpt-5.4-mini`, `gpt-5.5`, `gpt-5.6-luna`, `gpt-5.6-sol`, and `gpt-5.6-terra`.
- Tools/context: direct CPA `previous_response_id` continuation, non-stream explicit tool replay, stream tool call, explicit stream replay, and a dedicated long-history response case. Public HTTP/SSE MUST NOT use tool-result `previous_response_id` as a 200 gate; HTTP `stream:true` is not Responses WebSocket v2.
- Item IDs: upstream-generated compliant replay and the version-sensitive overlong message-ID case.
- Usage/billing: positive and consistent input/cached/output fields, exactly one marked usage row, one distinct request ID, one dedup row, no legacy duplicate ledger row, and no negative cost.
- Logging: three controlled requests carry fixed synthetic body and session canaries. The gate permits at most 70 bytes per access line against the 58-byte baseline plus 20%, and requires zero new Authorization/Cookie/API-key/Bearer values, zero canary exposure, zero `X-CPA-TRACE-ID` names/values, and zero raw `resp_`/`call_`/`msg_`/`sess_` identifiers in CPA journal or any normal/error log bytes appended during validation. It also requires no permission change on existing logs and no world-readable new log. Any `journalctl`, `tail`, or `find` read failure is blocking rather than interpreted as zero matches.

Each case has one shared retry budget. One retry is allowed when one of these independent conditions holds: (a) HTTP `429/500/502/503/504` or a transport exit `28/35/52/56/124/137/143/255` is classified as an established transient/upstream class such as `upstream_error`, `api_error`, `server_error`, `response.failed`, `upstream_failure`, `server_is_overloaded`, `service_unavailable_error`, `overloaded_error`, `rate_limit_error` or `temporarily_unavailable`; or (b) one malformed SSE terminal framing result is exactly `dual_terminal`, `invalid_terminal_count`, or `invalid_terminal_order`. The final successful attempt must still contain exactly one terminal event, with the terminal event last. Global auth `success/failed` counters and Trace coverage budget never consume this retry; Trace deadline/coverage failures are contract failures and are not reclassified as transport retry. `401/403`, `invalid_grant`, invalid credentials, refresh failures, and no-available-account conditions are hard authentication failures and are never retried. Any other counter structure, repeated transient, another contract failure, or a different failure after a consumed retry is blocking and returns non-zero to the still-armed controller. No third case attempt exists. The production-used model sweep is stricter and does not retry.

## Short Observation Thresholds

The validator compares the same 15- or 30-minute baseline/post window:

1. Count each public endpoint/status signature with its endpoint total and sample size. Error signatures are built only from non-loopback `ops_error_logs` rows so direct localhost/tunnel validation cannot offset Nginx public 5xx; the live preflight must confirm public traffic is recorded as non-loopback before execution.
2. New or unclassified 5xx must be at most 1%.
3. A pre-existing 5xx signature must not exceed the greater of its refreshed baseline rate plus 2 percentage points or 1.5 times its refreshed baseline rate, capped at 100%.
4. The aggregate allowance does not override the critical replay rule; final Responses, Chat, tools, direct account-pool, and process/config cases must pass under the one-known-error retry policy before disarm.
5. P95 duration and TTFT must not exceed 120% of the same-window old-version baseline.

Any violation exits non-zero and automatically invokes rollback before the controller can disarm.

Passing immediate validation and the applicable 15/30-minute observation is the terminal success gate for this execution; no post-switch 24-hour observation is required. The fixed `v7.2.82` rollback backup must remain present with its reviewed digest after success, and this change does not clean up or delete it.

## Rollback Contract

The rollback script is idempotent. If verified `v7.2.82` is already active and listening, it exits successfully without disruption. Otherwise it:

1. Acquires the shared lock and refuses mutation when a different controller owns the window. The controller uses `require-match` until claim success is confirmed, so an absent owner after an unconfirmed claim performs no mutation; after confirmed ownership or a release attempt it uses `allow-absent`, so unexpected owner loss is recovered conservatively. A manually authorized independent rollback defaults to `allow-absent`.
2. Starts a fresh 60-second recovery deadline.
3. Stops with `--no-block`, applying the same five-second control-group kill policy if required.
4. Installs the fixed backup through a same-filesystem temporary file and verifies its hash before rename.
5. Starts with `--no-block` and verifies active, `8317`, formal SHA-256, exact `v7.2.82` version line, and remaining deadline.
6. Removes only the matching owner token after verified recovery and leaves config, unit, Sub2API, Nginx, database, and the current OAuth directory unchanged.

After rollback, direct CPA, Sub2API localhost, public Responses, model, account-pool, and token-usage checks must be repeated before declaring the service recovered.
