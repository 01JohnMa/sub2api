# CPA v7.2.104 Switch Attempt - Live Pool Capacity - 2026-08-02

## Result

The fifth controlled production attempt did not complete the upgrade. The fixed `v7.2.104` candidate passed the process gate, then the immediate validator stopped at `live_contract_failure`. The still-armed controller automatically restored the fixed `v7.2.82` binary and verified active service, listener, formal SHA-256, and exact version. Production remains on `v7.2.82`.

No config, systemd unit/drop-in, OAuth file, account weight, routing mode, session affinity, plugin setting, Sub2API image, Nginx config, database data, or rollback backup was changed.

## Timeline

The timestamps below are UTC on `2026-08-02`.

- Controller run: `cpa-v104-20260802T103740Z-88153-1367e23d-8476-4641-b127-a3ff9aecb4fb`.
- `10:38:38`: systemd stopped `v7.2.82` and started the verified `v7.2.104` candidate.
- The candidate passed active/listener/hash/exact-version validation.
- The immediate validator returned `live_contract_failure` before the later business cases or short observation could run.
- `10:39:10`: automatic rollback stopped the candidate.
- `10:39:11`: systemd started the verified `v7.2.82` rollback binary.

The candidate was active for about 32 seconds. This record does not claim zero request impact during either service transition.

## Redacted Reproduction On Unchanged v7.2.82

Three low-volume replays after rollback kept the same three anonymous Codex identities and returned HTTP `200` for the selected model, Responses first/continuation, Chat, and all round-robin probes. In every replay, disabled/unavailable/runtime-only and hard-auth counts remained zero. The anonymous success-delta vectors were:

- `[0,2,6]`
- `[0,0,6]`
- `[0,1,9]`

The corresponding failed-counter samples included `[0,1,1]` and `[0,2,2]`; the failed field remains telemetry only under the existing contract. These old-version reproductions establish that a short controlled sample can miss one or two per-identity successes while every client-facing request still succeeds through cross-credential failover.

## Account-Pool Diagnosis

The post-rollback management snapshot contains one aggregate `active` entry and two aggregate `error` entries. All three entries remain `disabled=false`, `unavailable=false`, and `runtime_only=false`; no `invalid_grant`, invalid credential, refresh failure, unauthorized, 401/403, or no-available-account signature was found.

The two anonymous `error` entries have the same structured error classification:

- error type: `service_unavailable_error`
- error code: `server_is_overloaded`

They continued to serve successful production requests while retaining the aggregate error status. In the `10:40-10:50` bucket they each recorded `13` successes; in `10:50-11:00` they recorded `19` and `21` successes. Failures remained present but their rate was falling. The active entry also continued to serve traffic.

Official CLIProxyAPI source defines aggregate `status=error` as a temporary error state. Actual selection blocking depends on auth/model availability and cooldown state, and a later success can clear the relevant model state while another model error keeps the aggregate status stale. A single client request may attempt more than one credential: failed attempts increment the failed account while the final successful attempt returns HTTP `200` through another account. Therefore the management UI's `error` label alone does not prove an invalid OAuth account, while per-identity success over a suitable observation window remains useful liveness evidence.

The current evidence identifies intermittent upstream overload across the Responses path, not an OAuth credential failure and not a confirmed `v7.2.104` regression. No OAuth login, reset, service restart, account edit, or routing change is justified by this evidence.

Official source references:

- https://github.com/router-for-me/CLIProxyAPI/blob/v7.2.104/sdk/cliproxy/auth/status.go
- https://github.com/router-for-me/CLIProxyAPI/blob/v7.2.104/sdk/cliproxy/auth/selector.go
- https://github.com/router-for-me/CLIProxyAPI/blob/v7.2.104/sdk/cliproxy/auth/conductor_cooldown.go
- https://github.com/router-for-me/CLIProxyAPI/blob/v7.2.104/sdk/cliproxy/auth/conductor_execution.go

## Stop Condition And Next Gate

Do not repeat the production switch using the old-version three-positive-success predicate: it is not stable or attributable under v7.2.82 cross-credential failover. Keep the current OAuth files and service unchanged until the reviewed candidate-only Trace gate is installed and verified. The next candidate attempt must confirm selected-model catalog presence for all three expected credentials, then cover all three v7.2.104 stable `auth_index` values with final successful Trace headers within the bounded 12-probe/30-second window. A persistent `unavailable=true`, stopped success growth, hard-auth signature, missing/unknown/duplicate Trace, or incomplete coverage remains blocking.

If a validator change is proposed to make the pool coverage sample less timing-sensitive, it must preserve the same final requirement that all three unchanged identities prove current selected-model liveness, receive independent read-only Review, rebuild the manifest/controller self-hash, and pass the full local, Linux snapshot, verify-only, and OpenSpec strict suites before another production attempt.
