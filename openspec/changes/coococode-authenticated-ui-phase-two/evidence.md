# Phase-Two Frontend Evidence

This file is the non-sensitive evidence index for the authenticated frontend work. It starts empty because no authorized user/admin baseline has yet been supplied for this change. Never record credentials, cookies, tokens, private URLs, production row contents, or personal data.

## Baseline Identity

| Field | Value |
| --- | --- |
| Base commit | `ecb7c2e3ae22686ffbcf0fac4e7a1ba98aeafcb9` |
| Official comparison | `v0.1.162` / `27f094e0960ebd8e52de7ff7e763c6fec2ff4057` |
| Candidate commit/image | Pending |
| Candidate health URL/result | Pending; expected loopback `/health` body is `{"status":"ok"}` |
| Synthetic user/admin fixture IDs | Pending; non-sensitive aliases only |
| Production baseline authorization/time | Pending |

## Route And Feature Baseline

For each authenticated route, add one row or a link to its grouped rows.

| Role | Route | Standard/simple/backend result | Sidebar visibility vs direct-route guard | Feature source/state | Safe interactions | Candidate commit/time | Result |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Pending | Pending | Pending | Pending | Pending | Read-only only | Pending | Pending |

## Issue-To-Batch Acceptance Matrix

Use stable IDs (`UI-P0A-001`, `UI-P0B-001`, `UI-P1-001`, and so on). Before/after rows must use the same role, fixture/data shape, feature state, and viewport.

| Issue ID | Severity | Role/route | Feature/fixture state | Viewport | Baseline observation | Owning batch | Frozen files | Observable acceptance | Before screenshot | After screenshot | Disposition |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Pending | Pending | Pending | Pending | Pending | Pending | Pending | Pending | Pending | Pending | Pending | Pending |

## Geometry And Browser Results

| Batch | Role/route | Viewport | Document client/scroll width | Table wrapper client/scroll width | Dialog bounds | Header/filter/table-header bounds | Console/API window and allowlist | Screenshot | Result |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Pending | Pending | Pending | Pending | Pending | Pending | Pending | Pending | Pending | Pending |

Existing errors may be allowlisted only with timestamp, route, source, reproduction, owner, and proof that the batch did not introduce them. Every release requires zero new or unowned console errors, unhandled rejections, and failed API requests.

## Frozen Source Scope

Record the output or artifact reference for:

```text
git diff --name-status ecb7c2e3ae22686ffbcf0fac4e7a1ba98aeafcb9...HEAD -- frontend/
git diff --stat 27f094e0960ebd8e52de7ff7e763c6fec2ff4057...HEAD -- <each-frozen-file>
```

| Batch | Accepted issue IDs | Frozen files | Shared consumers | New upstream-owned file? | Reviewer decision |
| --- | --- | --- | --- | --- | --- |
| Pending | Pending | Pending | Pending | No | Pending |

## Commands And Results

| Batch | Focused Vitest paths/result | lint/typecheck/full Vitest/build | dependency/OpenSpec checks | Exact candidate start/seed/health/teardown | Logs/health/restarts | Result |
| --- | --- | --- | --- | --- | --- | --- |
| Pending | Pending | Pending | Pending | Pending | Pending | Pending |

The exact candidate record includes sanitized compose/env artifact paths, image tag/digest/embedded commit, loopback URL, disposable PostgreSQL/Redis identity, synthetic role aliases, literal start command, fixture seed command, health body, and teardown confirmation. Secret values are omitted.

## Independent Reviews

| Batch | Review input commit/diff | Validation evidence | Critical | Important | Minor | Decision |
| --- | --- | --- | --- | --- | --- | --- |
| Plan revision | This OpenSpec change | Strict validation and diff check passed; two independent read-only reviewers approved | 0 open | 0 open | 2 wording/status findings fixed | `APPROVE_PLAN` |

## Release And Rollback

| Batch | Exact commit | CI run | OCI index / amd64 digest | Previous production image | Backup path/check | Deploy command/result | Smoke/log/browser result | Rollback/post-rollback health |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Pending | Pending | Pending | Pending | Pending | Pending | Pending | Pending | Pending |

The authoritative procedure is the `Production Preflight And Cutover` section of `openspec/changes/upgrade-sub2api-v0-1-162/evidence.md`. Documentation-only commits do not authorize or trigger a production replacement.
