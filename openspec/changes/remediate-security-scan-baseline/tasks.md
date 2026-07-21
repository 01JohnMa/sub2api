## 1. Advisory and Dependency Verification

- [x] 1.1 Confirm primary advisory records and minimum fixed versions for Go, AWS EventStream/S3, axios, and form-data
- [x] 1.2 Inspect the resolved frontend/backend dependency graph and direct dependency declarations
- [x] 1.3 Determine whether `xlsx` has a supported compatible fixed path; stop for explicit direction if only risk acceptance or a broader replacement is available

## 2. Backend Security Baseline

- [x] 2.1 Align the Go patch version in `backend/go.mod`, all release Dockerfiles, and CI/release assertions
- [x] 2.2 Upgrade the AWS S3/EventStream dependency chain to the minimum compatible fixed versions and regenerate `go.sum`
- [x] 2.3 Run backend tests and `govulncheck ./...`

## 3. Frontend Security Baseline

- [x] 3.1 Upgrade axios and form-data outside their affected ranges and regenerate `pnpm-lock.yaml`
- [x] 3.2 Apply the explicitly approved `xlsx` remediation or exception decision
- [x] 3.3 Run frontend lint, typecheck, complete unit tests, production build, and the repository production-audit exception checker

## 4. Release Review and CI

- [x] 4.1 Inspect the final diff and perform a read-only security review; resolve all Critical and Important findings
- [x] 4.2 Commit and push only the scoped security baseline and OpenSpec files
- [ ] 4.3 Require CI, Security Scan, and image build to pass for the same commit
- [ ] 4.4 Verify the GHCR tag, source commit metadata, `linux/amd64` manifest, and immutable digest

## 5. Production Promotion and Verification

- [ ] 5.1 Re-check live topology, container health, current image ID, and rollback files before mutation
- [ ] 5.2 Back up current compose/Nginx files and replace only `sub2api` with the verified registry image
- [ ] 5.3 Verify origin and public health, routes, auth/API behavior, and authenticated responsive pages
- [ ] 5.4 Record deployment evidence or execute and verify rollback on any failed cutover check
