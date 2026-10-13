## Evidence Scope

This file records redacted pre-switch evidence collected on 2026-07-29. It contains no API keys, management keys, auth filenames, emails, cookies, request bodies, response text, raw response IDs, raw `call_id` values, or private URLs.

The evidence is valid only while the production CPA binary, CPA configuration, OAuth directory, Sub2API image, Nginx configuration, and release target remain unchanged. The short-window traffic baseline must be refreshed immediately before an authorized switch.

Status update: the first authorized switch attempt has already been used and failed closed. Production was restored to verified `v7.2.82`; the authoritative execution record is `switch-attempt-20260731.md`. This preparation evidence does not authorize another execution.

## Release And Candidate

- Official latest was rechecked on `2026-07-31` Asia/Shanghai and had advanced to `v7.2.110` (released `2026-07-30 08:38 UTC`, commit `a80e808`). Per the fixed-target contract, this was recorded without changing the approved candidate. The `v7.2.104` release page remains available and reports release time `2026-07-28 08:35 UTC`, commit `c9417c8`.
- Fixed asset: `CLIProxyAPI_7.2.104_linux_amd64.tar.gz`.
- Official and downloaded archive SHA-256: `993babb37b6de831600f0eb31527ca0f938337e1d1f837d5cf846263affa9724`.
- Candidate path: `/opt/cpa/staging/v7.2.104/extracted/cli-proxy-api`.
- Candidate binary SHA-256: `6355d7424394f22293f9d9c8cb3b9ca0073734dc50e8b740bb2af5cea98aaf64`.
- Candidate version: `7.2.104`, commit `c9417c8a`, built at `2026-07-28T08:36:09Z`.
- Candidate is a dynamically linked Linux x86-64 ELF. `ldd` reported no missing libraries on the production host (GLIBC 2.39; release baseline GLIBC 2.17).
- Candidate owner/mode is `root:root 0755`, staging is `root:root 0700`, and no candidate process was started.

Official sources:

- https://github.com/router-for-me/CLIProxyAPI/releases/latest
- https://github.com/router-for-me/CLIProxyAPI/releases/tag/v7.2.104
- https://github.com/router-for-me/CLIProxyAPI/releases/download/v7.2.104/checksums.txt

## Production Runtime

Last invariant check: `2026-07-30T01:25:44Z`.

- Service: `cpa.service`, active and enabled, restart count `0` during baseline.
- Main PID: `2435873`, unchanged throughout preparation.
- Formal binary: `/opt/cpa/bin/cli-proxy-api`.
- Formal version: `7.2.82`, commit `f583414f`, built at `2026-07-17T08:17:41Z`.
- Formal SHA-256: `e2ea31a127240f213d046567721839d725eeb4c136225eef139c6a46aa464b17`, unchanged after backup, download, and all baseline requests.
- Systemd fragment: `/etc/systemd/system/cpa.service`, SHA-256 `9190014d8a8ab0331b6f188e4c98b0dfdc6049c714c145f531a888554c7b1111`; `DropInPaths` is empty and `ExecStart` resolves to the formal binary with `/opt/cpa/config.yaml`.
- Listeners: `127.0.0.1:8317` and `127.0.0.1:8080` remained present.
- Host: x86-64, GLIBC 2.39; `/opt/cpa` filesystem had about 14.7 GB free before preparation.
- Sub2API immutable image ID: `sha256:3fcff154fc43cfd6306bcfe1c0f61ff98caca6e98e56204dae43a49de274f899`.

## Configuration And Storage Gates

- Routing strategy: `round-robin`.
- `session-affinity=false`.
- Plugins configuration section absent; no plugins directory was found by the runtime audit.
- GitTokenStore configuration absent.
- Legacy `plugins.sync-revision` key absent.
- Debug disabled; file logging enabled with a 200 MB configured cap.
- Three Codex OAuth files exist in the configured auth directory.
- No configuration, OAuth file, unit, Nginx configuration, Sub2API image, or database row was modified.

## Rollback Backup

- Primary rollback directory: `/opt/cpa/backups/cpa-v7.2.82-20260729T111659Z`, `root:root 0700`.
- Backup binary SHA-256 equals the formal `v7.2.82` SHA-256.
- Backup binary reports `v7.2.82`, is executable, and is `root:root 0700`.
- Config copy is `root:root 0600`.
- The backup includes the unit file and redacted systemd metadata for ExecStart, fragment/drop-ins, EnvironmentFiles, PID, state, and restart count.
- Existing `v7.2.61` formal backup remains present as secondary disaster recovery material.
- Old staging files were not used as the primary rollback source.

## Account Pool And Scheduling

Original preparation management snapshot:

- Auth files: `3`, all provider `codex`.
- Status: `3 active`, `0 unavailable`, `0 disabled`, `0 runtime-only`.
- Anonymous cumulative success counts: `[760, 1362, 1384]`.
- Anonymous cumulative failure counts: `[6, 7, 146]`.
- One credential has materially more historical failures, but it was active and continued to receive successful requests during the baseline.
- A short controlled request sequence produced anonymous success deltas `[2, 3, 3]` and failure deltas `[0, 0, 0]`, proving that all three credentials were schedulable and that the current non-affinity path rotated requests.

Authorized-switch preflight on `2026-07-31` found aggregate status alternating between `1-2 active` and `1-2 error`, while disabled, unavailable, and runtime-only counts all remained zero. Two independent controlled sequences produced success deltas `[2, 2, 3]` and `[2, 3, 3]`, each with failure deltas `[0, 0, 0]`. No `invalid_grant`, 401/403, expiry, refresh-failure, or timeout class was found in the non-active status metadata.

Official `v7.2.104` source defines `StatusError` as a temporary error state, but scheduler blocking is determined by auth/model `Unavailable` and cooldown state. A model-scoped failure can retain aggregate `status=error` while the auth remains schedulable for other models; a later success clears only the successful model state. The immediate validator therefore follows the OpenSpec schedulability contract: it permits only aggregate `active`/`error`, requires unavailable/disabled/runtime-only counts of zero in both the before and after snapshots, requires both management reads to return HTTP 200, requires the same three unique anonymous identities in both snapshots, and confirms selected-model catalog presence for all three expected credentials. In candidate `v7.2.104` mode, dedicated final-success probes must cover all three expected anonymous `auth_index` values through valid `X-CPA-TRACE-ID` headers within 12 attempts and an absolute 30-second deadline; the `v7.2.82` legacy rollback path does not require that header. The global `success` and `failed` fields are cumulative and include concurrent traffic, request-scoped failures, and cross-credential failover, so their sorted deltas are retained only as three-element non-negative telemetry vectors; their magnitude never causes pass/fail or retry. A redacted hard-auth match count from `status_message` must be zero before and after, while actual selected-model HTTP/model/text/token checks remain mandatory. `pending`, `refreshing`, `unknown`, `disabled`, an identity-set change, post-request unavailability, hard-auth status, counter regression, malformed counter structure, missing/unknown/duplicate Trace, or incomplete Trace coverage remains blocking.

Official source references:

- https://github.com/router-for-me/CLIProxyAPI/blob/v7.2.104/sdk/cliproxy/auth/status.go
- https://github.com/router-for-me/CLIProxyAPI/blob/v7.2.104/sdk/cliproxy/auth/conductor_cooldown.go

This supports the distinction discussed with the user: each send is a new HTTP request, while logical context and prompt caching can continue even when CPA selects a different OAuth credential.

## Models And Traffic Shape

- Direct CPA model count: `25`.
- Direct CPA GPT-5.6 IDs: `gpt-5.6-luna`, `gpt-5.6-sol`, `gpt-5.6-terra`.
- Models used in the latest 24-hour Sub2API snapshot: `codex-auto-review`, `gpt-5.4`, `gpt-5.4-mini`, `gpt-5.5`, `gpt-5.6-luna`, `gpt-5.6-sol`, `gpt-5.6-terra`.
- Latest 24-hour successful usage by endpoint included `/v1/responses` (about 7.8k rows) and a small `/v1/chat/completions` sample.
- WebSocket usage rows: `0`.
- Image usage rows: `0`.
- No real Codex Live, sideband, WebRTC, or image-heavy WebSocket traffic was found. Path matches were an admin realtime-traffic page and external `/live/.env.*` probes, not inference traffic.

## Thirty-Minute Metrics

One Sub2API database snapshot reported:

- Successful usage rows: `394`.
- Internal ops error rows: `46`; these include failed attempts/retries and are not equivalent to final client failures.
- P95 total duration: `41463.30 ms`.
- P95 recorded TTFT: `5459.40 ms`.
- Input tokens: `2,925,410`.
- Cached tokens: `55,841,280`.
- Output tokens: `208,608`.
- Streaming successful rows: `378`.

The cached-token aggregate confirms that high cache usage is compatible with `session-affinity=false` and round-robin account rotation.

An Nginx final-status snapshot over 30 minutes reported:

- `/v1/responses`: sample `51`; `48 x 200` (`94.12%`), `1 x 502` (`1.96%`), `2 x 503` (`3.92%`).
- `/v1/chat/completions`: sample `30`; `30 x 503` (`100%`) in the same window.

These are pre-existing failure signatures. The post-switch comparison must keep each signature within the formal endpoint/status tolerance below and must separately count new/unclassified failures. It must not treat internal `ops_error_logs` rows as the final request error rate.

The short observation comparison is endpoint/status specific. For each signature it records total requests, count, rate, and sample size. New or unclassified 5xx must be at most 1%; an existing signature must not exceed the greater of its refreshed baseline rate plus 2 percentage points or 1.5 times its refreshed baseline rate, capped at 100%. This aggregate allowance does not override the critical-replay rule: two consecutive failures remain blocking.

## Stable Request Matrix

### Normal Responses And Chat

- Direct CPA Responses: HTTP 200, expected model, non-empty output, normal usage fields.
- Direct CPA Chat Completions: HTTP 200, expected model, non-empty output, normal usage fields.
- Direct CPA `previous_response_id` continuation completed with HTTP 200 in the direct baseline.
- Sub2API localhost and public Responses both completed with HTTP 200 for the same request: input `4690`, cached `3840`, output `8`, reasoning `0`.
- A Chat request returned localhost 502 and then public 200 when repeated sequentially. Combined with the Nginx and auth evidence, this is an existing intermittent upstream/account-path failure, not a fixed Cloudflare failure.
- On the Sub2API HTTP path, `previous_response_id` tool-result continuation returned 400 with the explicit contract message that it is supported only on Responses WebSocket v2. HTTP acceptance must therefore use explicit replay; it must not treat this 400 as an upgrade regression.

### Controlled Billing And Deduplication Proof

- A uniquely marked `gpt-5.6-sol` Responses request completed with HTTP 200.
- Response usage was input `4691`, cached input `3840`, output `10`.
- The exact marker matched one usage row, one distinct request ID, and one `usage_billing_dedup` row.
- The database split input as uncached `851` plus cached `3840`, equal to the response input total `4691`; output remained `10`.
- Billing classification was type `0`, mode `token`; total and actual cost were both `0.0064750000`.
- The legacy `billing_usage_entries` table had zero rows for this path. This matches the current production repository behavior, which claims the request in `usage_billing_dedup` and applies billing effects without inserting that legacy ledger row.
- No raw request ID, account identifier, key, request body, or response text was retained in this evidence.

### Tools

- Non-stream function call completed with HTTP 200; function name and JSON arguments were valid.
- The baseline function-call items had a non-empty `call_id` and no namespace field; the explicit-replay gate records this empty namespace as the expected current contract and blocks an unexpected namespace drift.
- Explicit replay of `function_call` plus `function_call_output` completed with HTTP 200 and non-empty final text.
- After the second-attempt correction, the public and tunneled tools cases both completed with HTTP 200, `status=completed`/one SSE terminal, exact synthetic tool-output marker consumption, non-empty `call_id`, and empty namespace as the current baseline contract.
- A stream tool attempt intermittently returned 502/`response.failed`; a controlled retry completed with HTTP 200 and `response.completed`.
- Stream function-call argument events reconstructed valid JSON.
- Explicit stream tool-result continuation completed with HTTP 200 and non-empty final text on the second allowed attempt; input `4773`, cached `4608`.
- Post-switch policy: one retry is allowed for a version-sensitive case; two consecutive failures or a new error class is blocking.

### Long History And IDs

- Long-history streaming Responses completed with HTTP 200 and `response.completed`.
- Measured header first-byte time: `1.869 s`; total: `8.242 s`.
- Usage: input `5651`, cached `3840`, output `9`.
- An upstream-generated message item ID of length `54` replayed successfully with HTTP 200.
- Replacing that ID with a synthetic length `84` ID caused current `v7.2.82` to return HTTP 502 `upstream_error`.
- Existing logs also contain a separate length `45` invalid message ID failure. Because a valid generated ID can be length 54, the old length-45 failure is a format/type contract problem, not a simple length threshold. It must remain a known baseline and must not be claimed as fixed without a matching replay.

## Logging Baseline And Security Boundary

- Three controlled Responses requests all returned HTTP 200.
- During the same concurrent window, CPA log files grew by `645` bytes while Nginx recorded `11` new access lines: approximately `58` CPA-log bytes per access line.
- CPA log file count stayed `14`.
- Existing error logs numbered `10` and totaled about `1.98 MB` during the first snapshot.
- Existing error logs may contain full request bodies and are currently more broadly readable than desired. This security issue predates the binary upgrade and is not modified in this change.
- Post-switch logging validation inspects field names, exact synthetic body/session canaries, raw session-ID prefixes, byte growth, modes, and redacted classifications; it must not copy complete log lines or bodies into evidence. The canaries cover the controlled requests without treating unrelated historical body-bearing error files as new evidence.

## Executable Control Validation

- Every `scripts/**/*.sh` file passed `bash -n`.
- `sha256sum -c scripts/manifest.sha256` passed for every manifest-fixed switch, rollback, validator, observer, log guard, and case file.
- `CPA_VERIFY_ONLY=1 scripts/controller.sh` passed its normalized controller self-check and fixed artifact manifest without collecting a baseline or entering the switch path.
- The macOS-compatible Bash timeout helper was exercised with a 100-second child; a TERM signal ended and waited for the active child in about one second with status `143`.
- The real production observer returned valid 15-minute Nginx endpoint/status JSON and Sub2API database P95/TTFT JSON without writing production state.
- The real baseline collector produced a root-only local file with valid 15- and 30-minute Nginx and usage snapshots.
- The concrete validator was run only through its first read-only process gate against current `v7.2.82`; it failed closed on the expected candidate SHA mismatch and did not enter any model request case.
- The authorized-switch Review found and blocked an after-snapshot race in the account-pool case before any production mutation. The fixed validator now rejects after HTTP failure, identity replacement/duplication, provider drift, `pending`, disabled, unavailable, and runtime-only states; its synthetic positive `active/error` case and eight negative cases passed. A management-only production probe confirmed three non-empty unique string identity keys and no disabled/unavailable/runtime-only account without sending a model request.
- The observation comparator passed when post data equaled the same-window baseline, failed for a new 5% Responses 503 signature, and failed when P95 exceeded the 20% limit.
- The billing query script returned valid zero-count JSON for a nonexistent marker; the earlier uniquely marked real request proves the one-row/one-dedup accounting contract.
- The all-log append-offset guard was exercised using a root-only `/run` snapshot. It found zero sensitive journal/appended-file matches, zero permission changes, and zero world-readable new files, then removed the snapshot; no log content was printed.
- The root `.gitignore` now has an exact exception for this change's `scripts/` path, and `git status --untracked-files=all` lists every authoritative script.
- The production coordination path was initialized and tested without touching the service: `/run/cpa-v104-switch/` is `root:root 0700`, `lock` is a regular `root:root 0600` file, and `owner` is absent after the tests. On exact active `v7.2.82`, one run ID claimed and released the owner successfully; a different run ID received status `75`; repeated release returned the ambiguous-absence status `77`. Rollback with `require-match` and no owner returned a verified no-mutation result, while a different-owner rollback returned `76`. `cpa.service` remained active with the same PID throughout.

## Completed Preparation Evidence

Completed and evidenced:

- Fixed official release, age, asset, and checksum.
- No GitTokenStore or legacy plugin sync key.
- Production runtime/config invariant.
- Formal `v7.2.82` rollback backup.
- Offline `v7.2.104` candidate verification.
- Models, account pool, Responses, Chat, tools, long history, item ID, token accounting, cache, and logging baselines.
- Candidate remained stopped and formal service remained active `v7.2.82`.
- The current Trace-coverage remediation is present under `scripts/`; its controller SHA-256 is `c2a251e52871c4d9dc4970481f0062a5d7beb287f90948ca96f1f1bacca93b43`, normalized self-check SHA-256 is `f2139bafaf148a2b129b61d03a07d630a7e3c15324ca46180a25d0fdcd0bb38f`, and fixed manifest SHA-256 is `be9e24f75f965a377ed8791c4f2f0292182120f3fe9b76fb65527e83ad859e6f`.
- The manifest fixes the remediated switch (`493435d4e84451a9994d2a5444ccc35e3fc820d7b1283108ae91ebafd0f2af47`), rollback (`9b61999a985937351f4088936c0280b1809f1ded99a61ca62419d1606dc153bc`), immediate/short-observation validator (`22911af70f2efb5fb4291440ade8e024863a9fef0a17da3428110fb794d91ba4`), validator runner (`bef4498382e6dda284208894aebe6e7de9ad5318475eff31bcb4ad3ea737ea7d`), live case (`5ceb2d9bc62581d379ecad74611351d8f573a6939206e8a75b2712ed7e007f03`), Responses path (`0ffada572c758be0208dcb1326ca6057a7350dd25dbe91e2f9161055e491de36`), tools case (`76ee8f691efc7a527f3c35e9befcd0178264fa84851f5f5c34ae682753b97118`), stream continuation (`720328fb5bd8368eff23ba08a8b22e906f8a37676c2cd4369ebe0b8faeda5586`), conditional-usage gate (`c587f7f5d5e1f6a7b286dd2598b668ea2d35c4fcf321cd6412304deb9bc92e12`), log validator (`7635712140978539f5e926e98a6be66b50fdbb07b2df8482909cc413e0535c34`), artifact snapshot/contract/log/Trace tests (`01bf1ef6080bc09f6beb59a2a5c4d52cae1cc670fff0529baae0509d70831854` / `e9ef23337634191e162980b65997b4603666da0a03fca5503840a64b7b504054` / `23e6a09fa632b9449e0bd8cb1d026f04172ccd69ab7afe78ad9ff7ab35cdc8b8` / `442cd06af0684762b771489176867bfa0263daa9b02ea658da7de93d31e62d04`), validator contract test (`6b67055fa9692402956e39a2f08cde24b9a72ba32a5ec5ee158cc5d5c9c26616`), owner claim/release, every dependency, every case script, and the remediation tests.
- The production unit reports 90-second start/stop defaults, `KillMode=control-group`, and `SendSIGKILL=yes`; the reviewed switch policy uses `--no-block`, polls absolute deadlines, and explicitly escalates a stop to control-group `SIGKILL` after five seconds.

The preparation-stage read-only Review closed Tasks 4.5 and 6.2 with Critical/Important/Minor findings all at zero. That conclusion applied before the first authorization and is superseded for execution purposes by the failed attempt and post-attempt Review recorded in `switch-attempt-20260731.md`.

Warnings to carry into the switch window:

- Existing intermittent Responses/tool/Chat failures require an immediate same-window baseline refresh.
- One credential has a much higher cumulative failure count. All three are currently schedulable, but aggregate status can remain `error` because of model-scoped failures; the candidate switch gate relies on unavailable/disabled state, zero hard-auth status matches, selected-model catalog presence, bounded 3/3 final Trace coverage, and structurally valid success/failure telemetry rather than global counter magnitudes.
- HTTP `previous_response_id` is not a valid Sub2API tool-continuation acceptance path; use explicit replay and the actually used WebSocket path only when real usage exists.
- `v7.2.104` request metadata logging needs a post-switch privacy and disk-growth check.
- Existing request-body error logs and permissions remain a separate security follow-up.

## Historical Post-Remediation Readiness (Superseded)

The pre-attempt artifacts passed their then-current `bash -n` checks, validator retry state-machine suite, manifest, controller verify-only/self-hash, Linux `/tmp` snapshot tests, OpenSpec strict validation, and Review; that Review was a historical checkpoint before the Trace remediation. The second attempt then exposed the HTTP/SSE `previous_response_id` mismatch documented in `switch-attempt-20260802.md`. After that attempt, the explicit-replay correction added exact tool-output, namespace, conditional-usage, artifact-snapshot, and body/session privacy-canary gates. The third attempt passed process/live/Responses/Chat/tools and rolled back at an intermittent malformed SSE terminal also reproduced on `v7.2.82`; the corrected runner permits one complete retry for the three malformed-terminal classes while requiring the successful result to remain uniquely terminated. The fourth attempt then reproduced a non-zero global `failed` delta on the unchanged old version and rolled back closed; `switch-attempt-20260802-live-counter.md` records the diagnosis and the new hard-auth/telemetry contract. The current Trace remediation then passed all local script tests, all `bash -n` checks, manifest verification, controller verify-only/self-hash, Linux OAuth snapshot tests, OpenSpec strict validation, a real `v7.2.82` legacy canary, and independent Reviews with Critical/Important/Minor all zero. This paragraph is historical preparation evidence and is superseded by the success record below.

The official release status was refreshed on `2026-08-02` Asia/Shanghai: latest is `v7.2.113`, while the fixed `v7.2.104` release remains available with HTTP 200. This does not change the frozen target.

At the fresh preflight on `2026-08-02` Asia/Shanghai, production remained active `v7.2.82` with PID `3990515`, `NRestarts=0`, listeners `8317/8080`, and no owner token. Formal/candidate/rollback/config/unit hashes and exact versions matched; the Sub2API immutable image was unchanged; no CPA/Nginx systemd job or CLIProxyAPI OAuth login/device flow was active. The 24-hour conditional-usage scan reported zero WebSocket-mode rows, special Live/sideband/WebRTC endpoint or user-agent rows, image rows, and video rows. No production write was used for this refresh.

The fourth authorization and subsequent failed Trace-gate attempts were consumed and are recorded above. That historical preflight observed verified `v7.2.82`; it is superseded by the successful third post-remediation attempt recorded in `switch-success-20260802-trace-gate.md`.

## Current Post-Switch State

The third post-remediation attempt completed on `2026-08-02` Asia/Shanghai. Production is now verified `v7.2.104` with active/listener/hash/version/process, immediate business, Trace 3/3, final live, public-header, log-privacy, 15-minute observation, owner-release, and backup-manifest evidence all passing. The user waived the post-switch 24-hour observation; no further switch is pending, and a future change requires new authorization and preflight.
