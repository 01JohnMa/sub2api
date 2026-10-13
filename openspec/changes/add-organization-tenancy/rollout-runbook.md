# Organization Reporting Dimension Rollout

## 1. Boundary

This runbook covers the first release of organization identity and organization-scoped statistics only.

- It does not authorize production deployment.
- It does not enable organization funding, allocation, reservation, ledger, settlement, provider evidence, reconciliation, or independent API Key quota.
- Existing personal routing, billing, quota, subscription, and Group behavior remain the baseline.
- Do not record API keys, authorization headers, provider credentials, request bodies, or private URLs.

## 2. Preflight

1. Confirm the additive schema is backward compatible and existing users/API Keys remain unorganized (`organization_id IS NULL`).
2. Confirm the existing personal request, usage writer, error writer, and report tests pass.
3. Confirm organization scope is derived from server-side User/API Key state; no client-provided organization value is trusted.
4. Confirm the release does not enable any organization spending capability or require organization balance tables.
5. Record release, migration, health, and test evidence without secrets.

## 3. Smoke Matrix

| Case | Expected result |
| --- | --- |
| Existing personal Key | Request and existing statistics behave exactly as before; organization attribution is NULL. |
| Organization member request | Existing request/routing/metering path is used; successful usage or attributable error stores the current organization ID. |
| Client-supplied other organization ID | The value is ignored for authorization and telemetry. |
| Organization A reads overview | Only A-attributed records and approved member summary are returned. |
| Organization A requests B object/statistic | Stable 404 with no existence disclosure. |
| Member removal | New requests use the personal path; previously stored A attribution remains unchanged. |
| Disabled organization | Organization-scoped reads are rejected according to the lifecycle contract; no spending or balance behavior is involved. |
| Pre-auth failure | Error remains unattributed (`organization_id IS NULL`). |

## 4. Evidence Queries

Use the repository's normal migration table and application diagnostics. For a two-organization fixture, verify the following relationships with secret-safe output:

```sql
SELECT organization_id, COUNT(*)
FROM usage_logs
GROUP BY organization_id
ORDER BY organization_id;

SELECT organization_id, COUNT(*)
FROM ops_error_logs
GROUP BY organization_id
ORDER BY organization_id;
```

For a request accepted before membership removal, verify its stored organization ID is unchanged after removal. For a request accepted after removal, verify it remains NULL/personal according to the existing telemetry contract.

## 5. Stop Conditions

Stop and do not enable organization statistics when:

- an existing personal request changes billing, routing, quota, subscription, or Group behavior;
- a client-provided organization value changes authorization or telemetry attribution;
- organization A can read B's object, count, pagination total, or aggregate;
- historical telemetry changes organization after membership removal;
- the release requires reservation, ledger, final evidence, or reconciliation to complete a normal request;
- any secret or request body appears in evidence.

## 6. Deferred Work

Organization funding, member allocation, independent spending, reservation, attempt/provider evidence, final execution evidence, ledger, settlement, pending reconciliation, and archive balance freeze require a separate reviewed OpenSpec change and separate rollout contract.
