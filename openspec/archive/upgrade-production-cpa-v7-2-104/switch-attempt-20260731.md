# CPA v7.2.104 Switch Attempt Evidence - 2026-07-31

## Result

The authorized production attempt did not complete the upgrade. The fixed `v7.2.104` candidate passed its process gate, but the first direct business case returned `live_contract_failure`. The still-armed controller automatically restored the fixed `v7.2.82` rollback binary and verified active service, listener, formal SHA-256, and exact version. Production remains on `v7.2.82`.

No config, systemd unit/drop-in, current OAuth directory, account weight, routing mode, session affinity, plugin setting, Sub2API image, Nginx config, or database data was changed.

## Fixed Artifacts And Run

- Controller SHA-256: `531dcd4cf7872e047545d6e9b6098e686c70c5429eb4a3bb31132771d1154a03`.
- Controller normalized SHA-256: `44617c97a6f8a7e157b65767c7db9324869047cdb000e6963f770da43302670a`.
- Manifest SHA-256: `3cd4ca491e190d76d084287f850f0f32f623a624bdb7a4375f1e4775e502e3f2`.
- Run ID: `cpa-v104-20260730T172704Z-55632-c4576033-7a7c-4ede-97d7-3fc3fa5d8955`.
- Root-only local execution log SHA-256: `4119956d159d4e7bc7613abc85b4c8ad6c373dae6b77014db5679c3292b19d1f`.
- An earlier invocation ended with SSH status `255` during baseline collection, before run ID output, owner claim, service stop, or production mutation. Production was verified unchanged before the reviewed controller was retried.

## Timeline And User Impact

All timestamps below are UTC on `2026-07-30`; add eight hours for Asia/Shanghai on `2026-07-31`.

- `17:27:20`: systemd began stopping `v7.2.82` and completed the stop in the same second.
- `17:27:22`: systemd started `v7.2.104`; the candidate passed active/listener/hash/exact-version process validation.
- `17:27:51`: the first direct business case failed closed and automatic rollback began.
- `17:27:52`: systemd completed the candidate stop.
- `17:27:53`: systemd started the restored `v7.2.82`.

The candidate ran for about 29 seconds. Total elapsed time from the first stop to verified rollback start was 33 seconds, below the 60-second recovery limit. There were two approximately two-second service transition windows. In the surrounding 50-second Nginx window, Responses recorded `10 x 200` and `2 x 502`; the two `502` responses occurred exactly at `17:27:20` and `17:27:52`, the two stop seconds. Chat recorded one `503` while the candidate was active; this is an existing pre-switch Chat signature, not a new signature established by this attempt.

## Final Production State

- `cpa.service`: `active/running`.
- Main PID after rollback: `3030514`.
- `NRestarts=0`.
- Formal SHA-256: `e2ea31a127240f213d046567721839d725eeb4c136225eef139c6a46aa464b17`.
- Exact version: `CLIProxyAPI Version: 7.2.82, Commit: f583414f, BuiltAt: 2026-07-17T08:17:41Z`.
- Listener count for `8317` and `8080`: `2`.
- Controller owner token: absent.
- Sub2API-local and public Responses both returned HTTP `200` with model `gpt-5.6-sol`, non-empty text, input tokens `4690`, and output tokens `8`.
- Sub2API-local and public Chat both returned HTTP `200` with non-empty text, input tokens `310`, and output tokens `8`.
- The production-model sweep had one TLS connection timeout before an HTTP response; its single retry passed all seven configured production model cases with HTTP `200`, non-empty output, and positive input/output token gates.

## Business-Gate Diagnosis

The same `live-direct.sh` case was rerun after rollback against `v7.2.82`. All direct model, Responses continuation, Chat, and three round-robin requests returned HTTP `200`. The before/after snapshots contained the same three Codex identities, all remained schedulable, and disabled/unavailable/runtime-only counts remained zero. Anonymous success deltas were `[1, 3, 6]`, but the global failure deltas were `[0, 0, 1]`.

Official `v7.2.104` source increments `Auth.Failed` for every execution result with `Success=false`, including request-scoped failures that do not make an auth unavailable. The counter also includes concurrent production traffic, so the current requirement that every global failure delta remain zero can fail on the unchanged old version even while all three identities prove current success and remain available. The validator correctly failed closed according to the reviewed runbook, but the runbook's zero-global-failure rule is stricter than the OpenSpec requirement to prevent reduced schedulable capacity, invalid credentials, or no-available-account behavior.

Source references:

- https://github.com/router-for-me/CLIProxyAPI/blob/v7.2.104/sdk/cliproxy/auth/conductor_cooldown.go
- https://github.com/router-for-me/CLIProxyAPI/blob/v7.2.104/sdk/cliproxy/auth/types.go

No retry is authorized until the validation contract is revised, tested, manifest-fixed, and independently reviewed.

## OAuth Snapshot Permission Incident

The switch created `/opt/cpa/backups/cpa-v7.2.104-switch-oauth-20260730T172720Z-3029693`. Although the script created the destination as `0700`, `cp -a auth-dir/. snapshot/` copied the source directory mode onto the destination. The resulting snapshot root was `0755`, and three of five regular files were group/other readable.

The exact snapshot created by this run was immediately hardened without reading content or changing the current auth directory. Final verification passed with:

- two directories, both `root:root 0700`;
- five regular files, all `root:root 0600`;
- zero symlinks or special files;
- zero group/other-readable or writable entries.

The original snapshot manifest covered only three root-level data files and omitted one nested data file. It was replaced with a recursive four-entry manifest covering every regular data file below the snapshot root, then verified silently with `sha256sum -c`. No file name, digest, or credential content was printed.

`switch-process.sh` remains unsafe for another attempt until it explicitly reapplies and verifies root-only ownership and modes after the copy.

## Stop Condition

Production is healthy on `v7.2.82`. Before any retry, the user must explicitly approve a revised validation policy and a corrected snapshot-copy implementation. Both changes require regenerated hashes, full local gates, and a new read-only Review.

## Post-Attempt Remediation

The two retry blockers were subsequently repaired locally without executing a production switch:

- the switch now copies into an `auth/` child protected by a permanently `0700` outer snapshot directory, rejects symlink/special entries with explicit `find` error propagation, uses a non-foreground process-group timeout, recursively enforces and verifies `root:root 0700/0600`, recursively manifests every regular data file, and silently validates the manifest under the existing absolute deadline;
- `live`/`final_live` may consume the case's sole retry only when every other gate passes and the first sorted global failure delta is exactly `[0,0,1]`; the complete retry must pass with `[0,0,0]`, and the retry cannot stack with a 5xx retry.

The remediated controller SHA-256 is `5dd01593caff8c30b9504a46d38d3ef78251686ddcea27bee2fabd9f2e1c05ca`, normalized SHA-256 is `a729addcebaaed9524326b7b1d4767efbe0bf58d27140f9aef683fdc836acd3a`, and manifest SHA-256 is `d998d61ca35c467c155115ddbf502903798efd0b8f56ea941a0dad3deb894313`. The new local/Linux gates and independent Review later passed, but these hashes do not reuse the first attempt's authorization and remain non-executable until a new explicit switch authorization.
