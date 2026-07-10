## 1. Isolation and Baseline

- [x] 1.1 Fetch origin and create isolated worktree branch `codex/fix-codex-manifest-cpa` from production commit `d601f4ac8a20c4bc9e35735bb4c61dc63410d50e` without touching user files in the main worktree.
- [x] 1.2 Record current production image, immutable digest, service health, source ref, public manifest failure, and direct CPA manifest success.
- [x] 1.3 Run focused existing backend manifest tests and frontend CC Switch/Use Key tests to establish the production-source baseline.

## 2. API Key Upstream Manifest

- [x] 2.1 Add failing tests for OAuth preservation, API Key upstream URL normalization, headers, proxy/error behavior, invalid manifest rejection, and no credential leakage.
- [x] 2.2 Implement API Key manifest fallback using the selected account's configured Base URL, API Key, proxy, timeout and existing HTTP safety patterns.
- [x] 2.3 Add account-whitelist filtering for API Key fallback manifests and disable upstream ETag reuse on filtered responses.
- [x] 2.4 Run focused backend tests for manifest service, handler routing, OAuth behavior and API Key fallback.

## 3. GPT-5.6 Client Defaults

- [x] 3.1 Update CC Switch OpenAI endpoint normalization and default model to `gpt-5.6-sol`; add focused deeplink tests.
- [x] 3.2 Update normal and WebSocket Use Key Codex templates to `gpt-5.6-sol`; update focused component tests.
- [x] 3.3 Run focused frontend tests, typecheck and production build.

## 4. Verification and Review

- [x] 4.1 Inspect the final diff for unrelated changes and confirm CPA, schema, credentials, Nginx and brand files are untouched.
- [x] 4.2 Run strict OpenSpec validation and record exact targeted verification results.
- [x] 4.3 Complete an independent read-only review; fix all Critical and Important findings and rerun affected checks.

## 5. Custom Image Release

- [ ] 5.1 Commit the reviewed implementation with a Chinese commit message and push the isolated branch/ref used by the custom image workflow.
- [ ] 5.2 Verify CI publishes `ui-redesign-<short-sha>` for the exact commit and record the immutable new digest.
- [ ] 5.3 Recheck live topology and rollback reference immediately before deployment.

## 6. Production Deployment and Acceptance

- [ ] 6.1 Pull the exact new image and recreate only the `sub2api` service; leave CPA, PostgreSQL, Redis, credentials and public routing unchanged.
- [ ] 6.2 Verify container/public health and absence of new startup or manifest errors.
- [ ] 6.3 Verify public Codex manifest contains Sol/Terra/Luna and excludes models outside the configured account whitelist.
- [ ] 6.4 Run one minimal `gpt-5.6-sol` request and one minimal `gpt-5.5` request through the production user-key chain.
- [ ] 6.5 If any critical check fails, restore the previous image digest and re-verify health; otherwise record the successful deployment evidence.
