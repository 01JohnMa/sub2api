## 1. Contract Tests

- [x] 1.1 Add a service test proving an absent setting returns a disabled configuration with a non-nil empty provider collection
- [x] 1.2 Add a service test proving non-not-found repository errors still fail
- [x] 1.3 Add an admin handler test proving an absent setting returns HTTP 200 with the existing success envelope

## 2. Service Implementation

- [x] 2.1 Handle only `ErrSettingNotFound` as the optional disabled default in `loadWebSearchConfigFromDB`
- [x] 2.2 Preserve the existing configured-value and unexpected-error paths

## 3. Local Verification and Review

- [x] 3.1 Run focused unit tests for the service and admin handler packages
- [x] 3.2 Run formatting, broader backend verification, and the existing frontend build
- [x] 3.3 Review the final diff and confirm no frontend caller, database schema, or unrelated runtime contract changed

## 4. Candidate and Release Evidence

- [x] 4.1 Verify the exact candidate returns HTTP 200 for the missing Web Search configuration and produces no Web Search warning in affected admin flows
- [x] 4.2 Publish and verify the exact reviewed Linux image through the existing release path
- [ ] 4.3 Deploy only the `sub2api` service after current production preflight and backup pass, then verify health, logs, API behavior, and rollback readiness
- [x] 4.4 Record the completed contract evidence in the parent phase-two change
