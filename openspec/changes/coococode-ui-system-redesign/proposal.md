## Why

The current Sub2API UI presents the default upstream brand and a generic teal SaaS visual system. coococode needs a distinct minimal pixel-coconut identity across public, authentication, user, and admin surfaces while preserving the proven gateway behavior already running in production.

## What Changes

- Introduce a coococode brand system with lagoon, reef, palm, coconut, shell, and coral tokens.
- Redesign the default public home page around a code-native pixel coconut mark, the existing terminal animation, and developer gateway messaging.
- Redesign authentication surfaces so login, registration, password reset, OAuth, agreement, Turnstile, and 2FA flows share the same brand treatment without changing auth behavior.
- Refresh the authenticated app shell, sidebar, header, shared cards, tables, forms, buttons, dialogs, empty states, and status badges so user and admin pages inherit one cohesive UI system.
- Keep all backend API contracts, database schema, permissions, billing, gateway routing, and production configuration semantics unchanged.
- Add production deployment and rollback documentation for replacing the current container image safely.

## Capabilities

### New Capabilities

- `coococode-brand-system`: Defines the visual system, public/auth experience, authenticated app shell, shared component styling, and deployment verification requirements for the coococode UI.

### Modified Capabilities

No existing OpenSpec capabilities are modified. This repository did not have existing specs before this change.

## Impact

- Affected code: `frontend/tailwind.config.js`, `frontend/src/style.css`, public/auth views, layout components, shared UI components, and brand component assets.
- Affected runtime: frontend bundle embedded into the Go binary through the existing Docker multi-stage build.
- APIs and data: no API, database, authentication, authorization, or billing contract changes.
- Operations: production deployment replaces the `sub2api` image after backup; rollback restores the previous image digest and compose configuration.
