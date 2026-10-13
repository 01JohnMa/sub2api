## 1. Contract

- [x] 1.1 Record the production evidence and scope boundaries.
- [x] 1.2 Define the no-schema, no-client-detail observability contract.

## 2. Tests

- [x] 2.1 Add failing service tests for classification, sanitization, deduplication, and cancellation.
- [x] 2.2 Add a failing handler test for an HTTP-200 Responses fallback with structured Ops context.

## 3. Implementation

- [x] 3.1 Add the service-level fallback event recorder using the existing Ops event JSON.
- [x] 3.2 Pass the real forwarding error into the Responses handler fallback without changing the public response.

## 4. Verification

- [x] 4.1 Run focused service, handler, and Responses lifecycle tests.
- [x] 4.2 Validate this OpenSpec change in strict mode.
- [x] 4.3 Inspect the final diff or equivalent file comparison and complete read-only review.

## 5. Production

- [ ] 5.1 Build and publish an immutable Sub2API image from the reviewed commit.
- [ ] 5.2 Deploy only after a separate production authorization and correlate a real failure end to end.
