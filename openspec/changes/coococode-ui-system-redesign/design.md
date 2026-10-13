## Context

At change inception, production ran `weishaw/sub2api:latest`, whose binary reported `Sub2API 0.1.133` at commit `68901cbfff783af794d96028be8dad3e532c0fe7`. The server container exposes a built Vue/Vite frontend embedded into the Go binary, so the redesign started from the upstream source at tag `v0.1.133`.

The frontend stack is Vue 3, Vite 5, TypeScript, Tailwind CSS 3, Pinia, Vue Router, and vue-i18n. The implementation will keep this stack and avoid adding new runtime UI dependencies.

## Goals / Non-Goals

**Goals:**

- Apply a cohesive coococode minimal pixel-coconut visual system across public, auth, user, and admin surfaces.
- Keep the dashboard useful as an operational tool: dense tables remain scannable, forms remain efficient, and status colors remain meaningful.
- Preserve all existing route guards, settings-driven feature flags, i18n keys, OAuth flows, Turnstile, 2FA, announcements, and onboarding behavior.
- Produce a custom Docker image that can replace the current production image and roll back cleanly.

**Non-Goals:**

- No backend API, database, authentication, authorization, billing, or gateway behavior changes.
- No replacement of Vue, Tailwind, Pinia, Vue Router, or Vite.
- No wholesale page-by-page product redesign in the first pass beyond key public/auth/app-shell surfaces and shared component styling.
- No fabricated testimonials, fake metrics, or new commercial claims.

## Decisions

- **Start from `v0.1.133`, not `main`.** This matches the production binary and avoids mixing a visual redesign with upstream functional changes. Alternative considered: current `main`; rejected because releases are moving quickly and would expand regression scope.
- **Use shared design tokens first.** Tailwind theme and global component classes will carry most of the redesign so all user/admin pages inherit the system. Alternative considered: manually restyle every view; rejected as too broad and brittle for a first production replacement.
- **Keep route and store contracts intact.** Public/auth/layout components may change markup and classes, but existing store calls, route names, guards, feature flags, and emitted events must remain intact. Alternative considered: new standalone frontend; rejected because login/session compatibility would be risky.
- **Use a code-native pixel coconut mark instead of a photorealistic hero image.** The public page keeps the existing terminal animation structure, while the brand mark carries the coconut identity. Alternative considered: generated island hero bitmap; rejected after visual review because it felt too realistic and de-emphasized the coconut.
- **Registry-built production replacement with rollback.** GitHub Actions builds the committed source and publishes an explicit `linux/amd64` GHCR tag. The production host only pulls and recreates `sub2api`; it does not compile or build release images. Mitigation is to back up compose/Nginx, record the current image digest, verify the registry manifest, smoke test, and keep rollback commands ready.

## Risks / Trade-offs

- **Large visual blast radius** -> Constrain implementation to tokens, shared components, app shell, and key public/auth surfaces; run broad frontend checks and browser smoke tests.
- **Tailwind class churn can hide regressions** -> Avoid unrelated refactors and preserve component props/events.
- **Tables may become less dense** -> Keep table typography compact and ensure admin pages remain scan-friendly.
- **Auth flow regressions** -> Preserve existing `LoginView.vue` logic, OAuth components, Turnstile handling, 2FA modal, and login agreement behavior.
- **Production replacement risk** -> The original `weishaw/sub2api@sha256:6054ebc4795528f2898cc2f591c683154cde4ace9def68a35f24747c0f9ed173` value is the historical change-inception baseline, not the 2026-07-21 cutover target. The actual cutover baseline and rollback files are recorded in task 6.4, and the executable rollback procedure is recorded in task 6.5.
- **Building on the production host can contend with the live service** -> Release images are built by GitHub Actions from committed source. Production only pulls a verified registry image and recreates the `sub2api` service with `--no-deps`.

## Migration Plan

1. Build and verify the frontend locally.
2. Commit and push the exact release source so GitHub Actions can build and publish a `linux/amd64` GHCR image.
3. Verify the registry manifest, image tag, commit metadata, and digest before touching production.
4. Back up production compose and Nginx files and record the currently running image ID.
5. Pull the registry image on production and replace only the `sub2api` service image reference.
6. Run `docker compose up -d --no-deps --force-recreate sub2api` in `/opt/sub2api`.
7. Smoke test `/`, `/login`, `/dashboard`, `/admin`, `/api/v1/settings/public`, and unauthenticated `/v1/models` behavior.
8. If smoke tests fail, restore both backed-up compose files, force-recreate only `sub2api`, and verify origin plus public health.

## Open Questions

None for the first implementation pass. The implementation assumes brand display name `coococode` and production domain `coococode.com`.
