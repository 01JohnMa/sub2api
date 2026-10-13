# Trace Gate Attempt

The next controlled `v7.2.104` attempt was authorized as part of the same confirmed execution window. The candidate process gate passed, but the first immediate `live` contract failed. The controller remained armed and automatically restored the fixed `v7.2.82` binary; active state, listener, rollback SHA, and exact rollback version were verified. The owner token was released by the controller cleanup path.

Observed facts:

- Run ID: `cpa-v104-20260802T122515Z-87468-ea1fa464-df57-49b6-9bdd-7e3380afe543`.
- Candidate process gate: passed.
- Immediate business result: `live_contract_failure`.
- Automatic rollback: passed; production returned to verified `v7.2.82`.
- No configuration, unit, OAuth file, Sub2API image, Nginx configuration, database row, or rollback backup was changed by the failed attempt.
- No hard-auth, overload, or HTTP 5xx signature was found in the redacted systemd/journal window.

This attempt does not establish whether the live failure was a missing/duplicate/malformed Trace header, incomplete three-index coverage, or another live-field contract. The controller previously retained only the generic validator error. A reviewed diagnostic-only change now emits a whitelist failure code or a Trace count summary without changing the pass/fail contract or retaining Trace values. The next candidate attempt must use that evidence; no Trace requirement is loosened.

A subsequent read-only synthetic probe against the unchanged `v7.2.82` path returned HTTP 200 with the selected model and positive tokens, and its redacted text classification was `exact_without_period` for the same instruction shape. This explains the candidate `trace_probe_text_mismatch`: the probe expected a trailing period that the model deliberately omitted. The candidate probe contract is corrected to request and match the marker without punctuation while remaining an exact full-text comparison.

Production remains on `v7.2.82` until a fresh preflight passes and a subsequent candidate attempt completes the existing immediate, 15/30-minute, process, public-header, log-privacy, backup, and owner-release gates.
