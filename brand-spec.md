# coococode Brand Spec

## Brand Position

coococode is an AI gateway and developer control surface with a quiet pixel-coconut identity. The visual language should feel like a precise operations console with a small, recognizable coconut mark: clear, breathable, restrained, and technical.

## Source And Scope

- Product baseline: `Wei-Shaw/sub2api` `v0.1.133` (`68901cbfff783af794d96028be8dad3e532c0fe7`).
- Display name: `coococode`.
- Domain in production: `coococode.com`.
- This redesign changes frontend UI/UX only. It does not change backend APIs, database schema, authentication behavior, authorization rules, billing logic, or gateway routing.

## Design System

- Ground: `#F8FAF7` shell white for app background.
- Surface: `#FFFFFF` and `#EEF6F2` for panels and subtle section bands.
- Ink: `#17231F` for primary text.
- Muted ink: `#5C6C66` for secondary text.
- Hairline: `#D8E1DB` for dividers and borders.
- Lagoon: `#0F7D73` for primary actions and active states.
- Reef: `#1D5D8F` for secondary information states.
- Palm: `#123B35` for dark surfaces.
- Coconut: `#8A6A4F` for warm metadata and low-priority accents.
- Coral: `#D96C4A` for destructive/warning emphasis only.

## Typography

- Display and body: `Avenir Next`, `Noto Sans SC`, `PingFang SC`, `Microsoft YaHei`, sans-serif.
- Mono: `JetBrains Mono`, `SFMono-Regular`, `Menlo`, `Monaco`, `Consolas`, monospace.
- Headings should be calm and medium-weight, not oversized marketing display type inside dashboards.
- Dashboard text should prioritize scanability, numeric alignment, and dense but breathable rows.

## Shape, Elevation, Motion

- Cards, inputs, dialogs, and tables use 6-8px radius.
- Pills and compact badges may use 999px radius.
- Elevation should be mostly hairlines and soft ambient shadows, never glassy glow-heavy cards.
- Motion should be short and utilitarian: 160-220ms, ease-out. Respect reduced motion.

## Pixel Brand Mark

- Primary mark: `frontend/src/components/brand/PixelCoconutMark.vue`.
- Use case: default logo fallback, public landing hero accent, and auth brand treatment.
- Style: minimal pixel coconut with a small code accent. The mark should be code-native and deterministic rather than photorealistic.
- The original terminal animation can remain; brand expression should come from the pixel mark, grid texture, restrained colors, and concise copy rather than a large atmospheric image.

## Anti-Patterns

- No generic purple/pink/blue AI gradients.
- No emoji-as-icon replacement.
- No photorealistic island hero image in the first viewport.
- No nested cards.
- No decorative orb/bokeh backgrounds.
- No fake metrics or testimonials.
- No backend or API behavior changes as part of the UI redesign.
