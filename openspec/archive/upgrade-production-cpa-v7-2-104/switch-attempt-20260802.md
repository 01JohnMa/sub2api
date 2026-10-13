# CPA v7.2.104 Switch Attempt Evidence - 2026-08-02

## Result

The second authorized production attempt did not complete the upgrade. The fixed `v7.2.104` candidate passed the process gate and the live, Responses, and Chat business cases. The tools case then returned `tools_contract_failure`; the still-armed controller automatically restored the fixed `v7.2.82` rollback binary and verified active service, listener, formal SHA-256, and exact version.

No config, systemd unit/drop-in, current OAuth directory, account weight, routing mode, session affinity, plugin setting, Sub2API image, Nginx config, or database data was changed.

## Fixed Artifacts And Run

- Controller SHA-256: `40d167d9894d35653b7cede29ee255722896f47135680de16ffedec3d8bef6e5`.
- Controller normalized SHA-256: `16db81c91e956dd9d46da10706b21ffc807b1654bd66100c3e680c717afbd30d`.
- Manifest SHA-256: `0f4b86c184021764beed3c5f31be9bc7d5b5bc3712f133e4825e48e083c36d66`.
- Run ID: `cpa-v104-20260801T161459Z-69008-2e1a863e-3fad-4a02-9212-f8a957d92122`.

## Timeline And User Impact

The timestamps below are UTC on `2026-08-01`; add eight hours for Asia/Shanghai on `2026-08-02`.

- `16:15:16`: systemd stopped `v7.2.82`.
- `16:15:17`: systemd started `v7.2.104`; the candidate passed active/listener/hash/exact-version validation.
- The validator reported `case_live=passed`, `case_responses=passed`, and `case_chat=passed`.
- `16:15:55`: the tools window recorded one non-stream `/v1/responses` 400 classified as `client_request` / `invalid_request_error`; the redacted error category referenced `previous_response_id`.
- `16:16:08`: automatic rollback began after `tools_contract_failure`.
- `16:16:09`: systemd started the restored `v7.2.82`.

The candidate ran for about 51 seconds. Each service transition completed in about one second. One public Responses 502 coincided with the first stop second; this record does not claim zero request impact during the transition.

## Diagnosis

The failure was caused by a validator contract mismatch. The established baseline already records that Sub2API HTTP tool-result continuation with `previous_response_id` returns 400 and is supported only on Responses WebSocket v2. HTTP `stream:true` is SSE, not WebSocket v2. The tools case nevertheless used `previous_response_id` for its public HTTP stream continuation and required HTTP 200.

This 400 is therefore not evidence of a `v7.2.104` regression. Direct CPA `previous_response_id` continuation remains a separate gate. Public Sub2API SSE and non-SSE tool continuations must use explicit replay of the `function_call` plus `function_call_output`; a real Responses WebSocket v2 gate is conditional on verified production use.

The switch-window aggregate contained no hard authentication error. The other observed 502/503 categories matched existing upstream/gateway classes and did not establish an account invalidation.

## Final Production State

- `cpa.service`: `active/running` on exact `v7.2.82`.
- `NRestarts=0`.
- Formal SHA-256: `e2ea31a127240f213d046567721839d725eeb4c136225eef139c6a46aa464b17`.
- Listener count for `8317` and `8080`: `2`.
- Controller owner token: absent.
- Sub2API: running and healthy.
- CPA `/health`: `status=ok`.
- Sub2API-local and public Responses both returned HTTP 200 with model `gpt-5.6-sol` and non-empty text after rollback.

## Stop Condition

No further production attempt is allowed until the explicit-replay correction, regression test, manifest/controller self-hash, verify-only, OpenSpec strict validation, fresh production preflight, and an independent read-only Review pass. The second authorization was consumed by this failed attempt; a new explicit switch authorization is required.
