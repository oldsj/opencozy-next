---
name: OpenCozy
description: A bright, calm family board where children see their day and tick it off, and parents keep it running from their phones.
# Colours (light theme) mirror web/design/src/tokens.css, the source of truth; `make design` fails when they differ.
colors:
  background: "#f3efeb"
  surface: "#fffdfa"
  surface-raised: "#ffffff"
  surface-soft: "#f8f5f1"
  surface-muted: "#eee9e4"
  foreground: "#342f2c"
  muted-foreground: "#665f5a"
  placeholder: "#6f6863"
  border: "#ded8d3"
  border-strong: "#c5bdb7"
  input: "#8c847f"
  ring: "#c2410c"
  primary: "#3f625b"
  primary-foreground: "#fffdfa"
  primary-soft: "#e6efeb"
  positive: "#2f8f5b"
  positive-ink: "#216440"
  positive-soft: "#e4f0ea"
  positive-line: "#98bda3"
  attention: "#e3a92b"
  attention-ink: "#765816"
  attention-soft: "#fbf4e3"
  attention-line: "#e0c790"
  danger: "#d9534f"
  danger-ink: "#963937"
  danger-soft: "#fae9e8"
  danger-line: "#dca59e"
  info-ink: "#2d55a0"
  info-soft: "#e6ecf9"
  info-line: "#9db0d2"
  profile-rose-soft: "#fae7ec"
  profile-rose-base: "#e0607e"
  profile-rose-ink: "#7d3647"
  profile-rose-line: "#dfaab1"
  profile-rose-track: "#eaced0"
  profile-rose-wash: "#f7eceb"
  profile-sage-soft: "#e7f2ea"
  profile-sage-base: "#5fa874"
  profile-sage-ink: "#31573c"
  profile-sage-line: "#abc7ad"
  profile-sage-track: "#d0ddce"
  profile-sage-wash: "#edf2eb"
  profile-sky-soft: "#e4f0fa"
  profile-sky-base: "#4a9be0"
  profile-sky-ink: "#275277"
  profile-sky-line: "#a3c1d8"
  profile-sky-track: "#ccdae3"
  profile-sky-wash: "#ebf1f3"
  profile-butter-soft: "#fbf4df"
  profile-butter-base: "#e3b52c"
  profile-butter-ink: "#645013"
  profile-butter-line: "#e0cc90"
  profile-butter-track: "#eadfbf"
  profile-butter-wash: "#f7f3e5"
  profile-lilac-soft: "#f0ecfa"
  profile-lilac-base: "#9b7fe0"
  profile-lilac-ink: "#55467b"
  profile-lilac-line: "#c3b6d8"
  profile-lilac-track: "#dcd5e3"
  profile-lilac-wash: "#f1eef3"
  profile-peach-soft: "#fdede6"
  profile-peach-base: "#ef8656"
  profile-peach-ink: "#77432b"
  profile-peach-line: "#e5b9a1"
  profile-peach-track: "#edd6c8"
  profile-peach-wash: "#f8efe8"
  profile-teal-soft: "#e0f2f1"
  profile-teal-base: "#2fa6a0"
  profile-teal-ink: "#195855"
  profile-teal-line: "#98c6bf"
  profile-teal-track: "#c6dcd6"
  profile-teal-wash: "#e9f1ee"
  profile-sand-soft: "#f4efe9"
  profile-sand-base: "#b8956a"
  profile-sand-ink: "#604d37"
  profile-sand-line: "#cfbfa9"
  profile-sand-track: "#e2d9cc"
  profile-sand-wash: "#f4f0ea"
typography:
  display:
    fontFamily: Fraunces
    fontWeight: 600
    note: SOFT 100. Names, headings, the clock, big numbers.
  text:
    fontFamily: Atkinson Hyperlegible Next
    fontWeight: 400
  label:
    fontFamily: Atkinson Hyperlegible Next
    fontWeight: 700
rounded:
  panel: 1.25rem
  control: 0.875rem
  small: 0.75rem
  pill: 999px
spacing:
  xs: 0.25rem
  sm: 0.5rem
  md: 0.75rem
  lg: 1rem
  xl: 1.5rem
  section: 2rem
---

# OpenCozy design direction

OpenCozy is designed from the product, not from the earlier app's CSS. The earlier app's features,
flows and screens (what each screen does, for whom, on which device) are the parity target. Its
palette (warm paper neutrals, one deep green) and its adult layout (a grouped side rail; every page a
serif title, one line of description, its main action top right, then sections) were kept on
purpose: families preferred them. The feature checklist is the rewrite inventory's screen list
(`docs/rewrite/old/ui-and-clients.md`).

## Who looks at it, and where

| Surface | Who | Device and distance | What it is for |
| --- | --- | --- | --- |
| **Wall display** (kiosk) | Children first, the whole family | Portrait touchscreen, about 1080 × 1920, always on, in an Android WebView of unknown age. Tapped at arm's length, glanced at from 3 m. | Today: who has what left, ticking it off, points, rewards, the TV, the calendar and meals. |
| **A child's iPad** (`/kid`) | One child | iPad, either orientation, iOS app browser view | Their own tasks, rewards, iPad-time bank, asking for more time. |
| **Parents' phones** | Adults | Phone, one-handed, light or dark as the phone is set | The inbox (approvals, TV prompts, time requests), quick fixes, pairing. |
| **Desktop browser** | Adults | Laptop or desktop | Setup and heavier management: chores, routines, rewards, devices, integrations, dashboards. |
| **TV** (dashboard view) | Everyone | Landscape, 3 m away, no touch | A chosen dashboard: calendar, meals, the home, weather. |

A child who cannot read well yet must still be able to find themselves, see what is left and tick it.
A parent must be able to clear the inbox with one thumb while doing something else.

## Principles

1. **People are the colour.** The chrome is quiet warm paper neutrals and one deep green. Each child's
   colour carries their column, avatar and progress, so a child finds themselves before reading.
2. **Big where it is touched, calm where it is read.** Child actions are 64px targets with generous
   gaps; ambient information is large type, not more boxes.
3. **Honest state, in words.** Saving, waiting for approval, failed, stale and offline each have their
   own words and look; colour never carries a state alone.
4. **Celebrate only what is confirmed.** A springy beat and the ta-da come only after the server
   confirms. Nothing moves to decorate.
5. **One system, three scales.** The same components on every surface; the kiosk and TV scale the
   whole rem-based system up rather than adding variants.

## Identity

### Colour

- **Neutrals**: warm paper (`background` #f3efeb, `surface` #fffdfa) and a deep warm brown-black
  ink (`foreground` #342f2c). Muted text passes 4.5:1 on every surface and soft fill.
- **Primary**: deep green (`primary` #3f625b), for the one main action per view and the current
  section (its `primary-soft` pill).
- **Focus**: a warm orange ring (`ring` #c2410c), 3px with a 3px offset, never the primary colour, so
  focus is visible on primary buttons and on every person colour.
- **Status lanes**: positive (green), attention (amber: saving, waiting for approval, stale), danger
  (red: failed, destructive), info (blue). Each has base, ink, soft and line; inks pass 6:1 on soft.
- **People**: rose, sage, sky, butter, lilac, peach, teal and sand (the names the server stores).
  Each has soft (their column's header), base (avatar, progress), ink (text on soft or wash, 7:1),
  line, track and wash (their task rows). All are precomputed: no `color-mix()`.
- **Dark theme**: the same tokens on a deep warm black, for phones in dark mode, TV views and the
  wall display at night (the app sets `data-theme="dark"`). Person colours stay recognisable:
  their soft fills become deep tints and their inks become light.

### Type

- **Fraunces** (soft display serif, `SOFT` 100) for names, headings, the clock and big numbers. It
  is warm and a little playful without being childish.
- **Atkinson Hyperlegible Next** for everything else. It was drawn for low-vision readers and keeps
  I, l and 1 and b and d apart, which helps early readers too.
- Both are the upstream variable TTFs, unmodified, under the SIL Open Font License; no subsetting.
- A fixed scale, in rem: clock 5, page 2, section 1.375, task 1.1875, body 1, label 0.9375, meta
  0.875. Numbers that change use tabular figures.

### Shape and depth

- Rounded and soft: panels 1.25rem, controls 0.875rem, chips and pills fully round.
- Depth is light: one soft shadow for raised panels, a stronger one for sheets and toasts. Lines
  separate within a panel; space separates panels.

### Motion

- State changes: 160ms ease-out. A confirmed tick: the check pops with a spring (520ms), the row
  settles, the points count up. No looping animation except a saving spinner.
- `prefers-reduced-motion`: every movement becomes an instant change or a short fade.

### Icons

Lucide, 2px stroke, always beside a word for anything a child must act on.

## Layout by surface

Set `data-surface` on `<html>`; it scales `--root-size`, so type, spacing, radii and targets all grow
together.

- **Kiosk** (`data-surface="kiosk"`, root 125%). A top bar with the clock and date; the people board
  fills the middle; family navigation is a bottom tab bar within a child's reach. One to three
  children: one column each. Four or more: two columns, each scrolling on its own. Household bands
  (calendar, meals, the TV) sit under the board. Settings hide behind a small icon for adults.
- **iPad** (`/kid`, default scale). One child's colour frames the page: their header, their tasks, a
  rewards shelf, and their iPad-time bank with "Ask for more time".
- **Phone** (default). Single column. The manage navigation is a slim bar at the top: the current
  section, a shortcut to the inbox with its count, and Menu, which opens every section in a bottom
  sheet. Bottom sheets for forms and confirmations.
- **Desktop** (default, from 1024px). A side rail for manage navigation, grouped Family, Home and
  Setup, with the adult's account and Sign out at its foot; content up to 72rem; side sheets.
- **TV** (`data-surface="tv"`, root 160%, usually dark). No controls; widgets in an ordered list that
  flows into columns; nothing smaller than body text.

## Components

Every screen is built from `@app/design`. Each component has a preview (`.design-sync/previews/`),
checked by axe in light and dark. Component APIs may change while screens are being written; changes
are agreed with the screen authors.

## Platform rules

- Tokens only: no hex, palette class, `dark:` or opacity modifier outside `tokens.css`.
- The wall display's WebView is of unknown age: no `color-mix()`, `@property` or cascade layers for
  component CSS (only element defaults sit in `@layer base`), `100vh` before `100dvh`, no
  container-query-only layouts.
- CSP: no inline styles or scripts, no `dangerouslySetInnerHTML`; QR codes are SVG paths from a
  module matrix.
- WCAG 2.2 AA. Targets at least 44px (`--target-min`), 64px for child actions (`--target-kid`). The
  focus outline is 3px.

## Do

- Let each person's colour lead; keep everything around it neutral.
- Say states in words a child understands: "Waiting for approval", "Couldn't save — tap to try again".
- Give every child action a 64px target and space around it.
- Show when information was last updated whenever it might be stale.

## Don't

- Don't add a colour, a shadow or a size outside the tokens.
- Don't celebrate anything the server hasn't confirmed, or play sound on a retry.
- Don't put management or credentials anywhere a child can reach on the wall display.
- Don't use colour alone for a person or a state.
