# Successful Trace-Gated Switch

The third post-remediation controlled attempt completed the frozen `v7.2.104` upgrade contract. The candidate process gate passed, immediate business validation passed, the 15-minute observation passed without needing the 30-minute extension, final cases passed, the owner was removed, and the controller disarmed automatic rollback only after all final gates succeeded.

Run facts:

- Run ID: `cpa-v104-20260802T124826Z-11315-fa9a322a-2bc3-4719-ae11-1482b5786b64`.
- Candidate: exact `v7.2.104` version and reviewed SHA; process/listener gate passed.
- Immediate cases: direct candidate live with selected-model catalog count `3` and candidate Trace coverage `3/3`; Sub2API Responses, Chat, tools, stream continuation, long history, and item ID passed.
- Additional gates: production model sweep, billing/token checks, log-rate check, 15-minute observation comparison, final Responses/Chat/tools/live, process invariant, and log privacy/permission checks passed.
- Final live and public-path checks retained the strict Trace contract; Sub2API localhost/public Responses had zero `X-CPA-TRACE-ID` headers.
- Owner: removed only after final gates; `/run/cpa-v104-switch/owner` is absent.
- Recovery: the fixed `v7.2.82` backup remains present. The switch-created OAuth snapshot was independently verified as root-only, regular-file-only, `0600` data under `0700` directories, and manifest-valid. No cleanup or deletion was performed.
- Sub2API immutable image remained unchanged and running; no 24-hour post-switch observation was run because the user explicitly waived it. Immediate plus 15-minute observation is the accepted success contract.

The two earlier post-remediation attempts remain recorded separately: both failed closed at the live contract and automatically restored verified `v7.2.82`; the second exposed the model's deterministic omission of a requested trailing period, which was corrected without weakening exact text or Trace coverage checks.
