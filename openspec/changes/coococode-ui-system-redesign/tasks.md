## 1. Baseline And Specs

- [x] 1.1 Check out `Wei-Shaw/sub2api` tag `v0.1.133` on branch `codex/coococode-ui-redesign`
- [x] 1.2 Initialize OpenSpec and create change `coococode-ui-system-redesign`
- [x] 1.3 Add `brand-spec.md`, proposal, design, spec, and tasks artifacts
- [x] 1.4 Validate OpenSpec with `openspec validate coococode-ui-system-redesign --strict`

## 2. Brand Asset

- [x] 2.1 Create a deterministic pixel coconut brand mark component
- [x] 2.2 Use the mark as the default logo fallback and public/auth brand accent
- [x] 2.3 Keep the original home terminal animation structure instead of replacing it with a photorealistic hero image

## 3. Frontend Design System

- [x] 3.1 Update Tailwind colors, fonts, shadows, radius, and motion tokens
- [x] 3.2 Update global component classes in `frontend/src/style.css`
- [x] 3.3 Keep existing component props, events, stores, and route contracts intact

## 4. Public And Auth Surfaces

- [x] 4.1 Redesign `HomeView.vue` around coococode brand messaging, pixel coconut mark, and terminal animation
- [x] 4.2 Redesign `AuthLayout.vue` with minimal pixel-coconut layout treatment
- [x] 4.3 Restyle login/register/password reset surfaces without changing auth logic
- [x] 4.4 Sanitize custom home HTML and isolate URL embeds without changing the `home_content` setting contract
- [x] 4.5 Localize all default home-page copy and keep the locale control legible under the global dark class
- [x] 4.6 Add localized accessible state to the login password visibility control

## 5. Authenticated Shell And Shared UI

- [x] 5.1 Redesign `AppLayout.vue`, `AppSidebar.vue`, and `AppHeader.vue`
- [x] 5.2 Restyle shared table, stat card, empty state, dialog, input, select, and status components where needed
- [ ] 5.3 Spot-check key user and admin pages for density, responsiveness, and feature flag behavior
  - Source/test checks are complete, but signed-in user/admin visual checks are still pending the same authenticated browser session required by task 6.3.

## 6. Verification And Deployment

- [x] 6.1 Run frontend typecheck, lint check, tests, and build
  - Typecheck, lint, all 730 frontend tests, and the production build pass.
- [x] 6.2 Build a custom Docker image and verify `/health`
  - Built `linux/amd64` image `ui-priority-20260721T014012Z`; an isolated local PostgreSQL/Redis smoke stack reported `running|healthy` and `/health` returned `{"status":"ok"}`.
- [ ] 6.3 Browser-check public, auth, user, and admin routes at mobile/tablet/desktop widths
  - Public/home and auth surfaces passed at 320, 390, 768, and 1440 px; unauthenticated dashboard/admin guards were verified. Authenticated user/admin pages still require a signed-in verification session.
- [x] 6.4 Back up production compose/Nginx files and record the current image digest
  - Backup: `/opt/sub2api/backups/20260721T022853Z-pre-ui-priority`.
  - Rollback image: `ghcr.io/01johnma/sub2api:ui-redesign-69cd045`, ID `sha256:ad162ceace8a4cdfe35933cd9399cee1fa4e381c39850953b63ac430bf5d7bc9`.
- [x] 6.5 Replace the production image, run smoke tests, and keep rollback commands ready
  - Historical interim step, superseded by the final registry promotion in 6.6; do not use this image or rollback reference as the current production state.
  - Interim production image: `ghcr.io/01johnma/sub2api:ui-priority-20260721T014012Z`, ID `sha256:7a1de03900c49ccd8a966fdc979264174c3942114843cd3f893aaa026cbe51d2`. This locally transferred tag is not the final registry promotion target.
  - Transfer archive SHA-256: `43be793becde711cc471ed574a1dae582ac6d457e4cfc7b6ca8db0e0561fba82`.
  - Origin/public health, public settings, routes, static assets, PostgreSQL, Redis, and CPA checks pass with zero container restarts.
  - Rollback procedure:

    ```bash
    cd /opt/sub2api
    docker image inspect ghcr.io/01johnma/sub2api:ui-redesign-69cd045
    cp -a /opt/sub2api/backups/20260721T022853Z-pre-ui-priority/docker-compose.yml /opt/sub2api/docker-compose.yml
    cp -a /opt/sub2api/backups/20260721T022853Z-pre-ui-priority/docker-compose.override.yml /opt/sub2api/docker-compose.override.yml
    docker compose -f docker-compose.yml -f docker-compose.override.yml up -d --no-deps --force-recreate sub2api
    docker inspect --format '{{.Config.Image}}|{{.Image}}|{{.State.Health.Status}}' sub2api
    curl -fsS http://127.0.0.1:8080/health
    curl -fsS https://coococode.com/health
    ```
- [x] 6.6 Commit and push the exact deployed source, publish a pullable `linux/amd64` GHCR image in GitHub Actions, replace the interim production image with the verified registry tag/digest, and repeat the scoped smoke and rollback checks without building on the production host
  - Final merge commit: `ecb7c2e3ae22686ffbcf0fac4e7a1ba98aeafcb9`; GitHub Actions run `29893129766` completed successfully.
  - Final production image: `ghcr.io/01johnma/sub2api:ui-redesign-ecb7c2e@sha256:2f7cc7bcdf2cc4b92ca79125b8417bf5a72c72c0c39433d000bd9d05396caf6e` (`linux/amd64`).
  - Production backup: `/opt/sub2api/backups/20260722T062047Z-pre-v0162`; only `sub2api` was recreated and its dependency services were not restarted.
  - Origin/public health, public settings, migrations, no-invalid-index check, stored forwarded-IP `false`, public/auth responsive pages, static frontend, expected unauthenticated `/v1/models` 401, and CPA state passed. Signed-in user/admin visual acceptance remains explicitly open in tasks 5.3 and 6.3.
