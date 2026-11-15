# OpenCozy product context

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

OpenCozy serves a household through a shared, large portrait touchscreen and responsive personal
browsers.

Children are the primary users of chores, routines, points, and reward redemption. Those journeys
must be understandable and usable without an individual login, another device, or a parent
assembling permissions.

The overall dashboard serves the whole family. Adults and children may use shared calendar, meal,
list, weather, camera, and other household widgets, so the product as a whole must not assume every
surface is child-focused.

Parents manage household setup, definitions, permissions, integrations, and sensitive actions
through authenticated adult views.

## Product purpose

OpenCozy is a self-hosted family dashboard that brings household coordination and children's daily
responsibilities into one web-first interface. Success means a family can use the shared display for
complete everyday journeys while authoritative state remains secure and persists across clients.

## Positioning

OpenCozy combines a child-usable family display with parent-controlled household management and
self-hosted data. Child actions, general household information, and configurable widgets share one
product without requiring children to have personal accounts.

## Operating context

- Where it is used, by whom (DESIGN.md gives each its layout):
  - **The wall display**: a large portrait touchscreen in a shared space, always on, in a kiosk
    WebView that may be older than current browsers. Children tick tasks and spend points; the whole
    family glances at the day from across the room. At night it may dim to a dark theme.
  - **A child's iPad** (`/kid`): one child's tasks, rewards, and iPad-time bank, in the iOS app's
    browser view.
  - **Parents' phones**: the inbox (approvals, TV prompts, time requests) and quick management,
    one-handed, in the iOS app or a browser, light or dark as the phone is set.
  - **Desktop browsers**: setup and heavier management.
  - **TVs**: a dashboard view read from across the room, without touch.
- A paired family display lets a child choose themselves, complete chores and routine steps, see
  points, and redeem eligible rewards.
- Confirmed chore and routine completions get brief celebratory feedback, such as a ta-da, alongside
  visible confirmation. Families can turn display sounds off, and failed or ambiguous actions never
  play success audio.
- Adults use authenticated management views for definitions, history, fulfilment, devices, screen
  time, integrations, and other privileged actions.

## Capabilities and constraints

- Stack: a Go API behind an OpenAPI contract, PostgreSQL, and a React single-page app (Vite,
  TanStack Router and Query, Tailwind 4 over the `@app/design` design system), shipped as one binary.
- The source is public. Committed content uses fictional data and portable configuration only.
- The server owns identity, household scope, permissions, transactions, request replay, and
  persisted state. Browser storage is limited to preferences or cache.
- Shared displays may perform explicitly allowed family actions but cannot perform adult management
  or receive provider credentials.
- The page runs under a strict Content Security Policy: no inline styles or scripts.
- Chore and routine interactions are kid-focused. Dashboard and management surfaces suit the broader
  household, including dense ambient information, live or stale source states, video aspect ratios,
  and direct household controls.
- The application distinguishes implemented behaviour from planned features, and simulated
  integrations from live ones.

## Brand commitments

The product name is OpenCozy. Its language is clear, friendly, and family-facing. Implementation
terms never leak into ordinary household flows. OpenCozy uses original code, styling, assets, and
branding.

## Evidence on hand

- This codebase is a rewrite of an earlier OpenCozy app with accepted household journeys: children,
  chores, routines, points, rewards, screen time, adult management, and revocable display sessions.
  Those journeys (features, flows, and screens: what each does, for whom, on which device) are the
  parity target. The earlier look is not: the interface is designed afresh (DESIGN.md).
- Automated fixtures are fictional. They establish no live household deployment, hardware
  acceptance, calendar provider, camera feed, Home Assistant integration, or weather integration.

## Product principles

- Make child journeys independently usable while keeping the whole dashboard useful to the family.
- Keep privileged management clearly separated and enforced by the server.
- Design for the shared portrait display first, then adapt deliberately to phones, desktops, and TVs.
- Deliver complete household journeys across UI, API, and persistence before broadening the roadmap.
- Show freshness, pending work, failures, and access boundaries in language families understand.

## Accessibility and inclusion

Large touch targets (44px everywhere, 64px for family navigation and child actions), visible focus,
keyboard-equivalent controls, clear state labels, WCAG 2.2 AA contrast, and responsive layouts.
Never rely on colour alone for person identity or status. Kid-focused interactions must not require
reading implementation language or understanding account and API concepts.
