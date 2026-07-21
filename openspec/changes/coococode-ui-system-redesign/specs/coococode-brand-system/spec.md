## ADDED Requirements

### Requirement: Brand identity is coococode pixel-coconut minimal

The frontend SHALL present `coococode` as the product brand and use the minimal pixel-coconut design system recorded in `brand-spec.md`.

#### Scenario: Public page shows coococode brand

- **WHEN** an unauthenticated visitor opens the default home route
- **THEN** the page presents `coococode` branding and pixel-coconut visual language instead of the generic Sub2API public landing design

#### Scenario: Public page keeps existing terminal motion

- **WHEN** the default home hero renders
- **THEN** the page uses a code-native pixel coconut mark while preserving the original terminal animation structure

#### Scenario: Brand system avoids generic AI visuals

- **WHEN** public or authenticated UI surfaces render
- **THEN** the UI avoids photorealistic island hero images, purple/pink/blue AI gradients, decorative orb backgrounds, emoji-as-icons, and fabricated metrics

### Requirement: Authentication flows preserve behavior

The frontend SHALL restyle authentication views without changing login, registration, password reset, OAuth, Turnstile, agreement, or 2FA behavior.

#### Scenario: Login form submits through existing auth flow

- **WHEN** a user submits valid login credentials
- **THEN** the existing auth store login path and downstream redirect behavior are used

#### Scenario: Optional auth controls remain settings-driven

- **WHEN** OAuth, Turnstile, login agreement, password reset, backend mode, or 2FA settings are enabled or disabled
- **THEN** the corresponding controls render according to the existing public settings and current component logic

#### Scenario: Password visibility control is accessible

- **WHEN** a keyboard or assistive-technology user operates the password visibility control
- **THEN** the control exposes a localized accessible name and its pressed state without changing login submission behavior

### Requirement: Public content remains safe and localized

The frontend SHALL render settings-driven public content with a constrained security boundary and SHALL localize the default coococode landing experience.

#### Scenario: Custom home HTML is sanitized

- **WHEN** an administrator configures HTML as `home_content`
- **THEN** executable or unsafe markup is removed before it is rendered in the application origin

#### Scenario: Custom home URL is isolated

- **WHEN** an administrator configures an HTTP or HTTPS URL as `home_content`
- **THEN** the embedded document has a descriptive title and an explicit sandbox policy that does not grant same-origin access

#### Scenario: Visitor changes the locale

- **WHEN** a visitor switches between Chinese and English on the default home page
- **THEN** navigation, hero, proof points, capability labels, and accessibility text update to the selected locale

#### Scenario: Home controls remain legible in dark preference

- **WHEN** the global dark class is active while the fixed-light public landing theme is displayed
- **THEN** the locale control retains accessible contrast against the landing-page background

### Requirement: Authenticated app shell is cohesive and operational

The frontend SHALL apply the coococode visual system to authenticated user and admin shells while preserving navigation, route guards, feature flags, and operational density.

#### Scenario: User and admin navigation remains available

- **WHEN** an authenticated user or admin opens the application
- **THEN** the sidebar, header, language switcher, theme toggle, balance/subscription controls, announcements, profile menu, and logout controls remain available according to the existing role and settings rules

#### Scenario: Dense admin pages remain scannable

- **WHEN** an admin opens table-heavy pages such as accounts, users, usage, ops, or settings
- **THEN** table, filter, badge, button, dialog, and empty-state styling remains compact enough for repeated operational use

### Requirement: Production replacement is reversible

The deployment SHALL record the current production image and back up service configuration before replacing the production container. The release image SHALL be built from committed source by CI, published to the registry for `linux/amd64`, and verified by tag and digest before production pulls it. The production host SHALL NOT compile or build the release image.

#### Scenario: Production rollout has rollback path

- **WHEN** the custom coococode image is deployed
- **THEN** the previous image digest and compose/Nginx backups are available so the service can be restored if smoke tests fail

#### Scenario: Production pulls a traceable registry image

- **WHEN** the final coococode release is promoted to production
- **THEN** GitHub Actions has published the committed source as a pullable `linux/amd64` GHCR image and production recreates only `sub2api` from that verified image
