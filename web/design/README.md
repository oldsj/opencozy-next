# Design system

OpenCozy's look: tokens, components and a preview of each, as a pnpm workspace package
(`@app/design`) that the web app imports. The rules and the reasons behind them are in
[`DESIGN.md`](../../DESIGN.md); who it is for is in [`PRODUCT.md`](../../PRODUCT.md). The same files
sync to a Claude Design design-system project, so designs made there use the components and tokens
the app ships.

```text
src/tokens.css            Every token as a CSS variable: OpenCozy's whole identity, and the only place a value is defined.
src/theme.css             Tailwind 4 theme: maps tokens to utilities (bg-surface, text-page, rounded-panel, ...). No values.
src/styles.css            What the app imports after Tailwind: tokens, theme, fonts, component CSS, element defaults.
src/components/*.tsx      The components (plain `oc-*` classes, each with its .css). Exported from src/index.ts.
src/fonts/                Lora and Work Sans (variable WOFF2) and their SIL Open Font Licenses.
src/test/                 Vitest tests of the components' semantics.
.design-sync/previews/    One <Component>.tsx per component: each named export is a story. fixtures.ts holds shared data.
.design-sync/config.json  design-sync settings (package shape, CSS entry, build command).
.design-sync/conventions.md  How to use the components, shown to Claude Design. Keep it current.
scripts/previews.ts       Builds every preview into dist/previews/<Component>.html, and dist/styles.css.
scripts/check.ts          The checks (below).
```

The app uses it like this (`web/src/index.css`, then any route):

```tsx
// index.css: @import 'tailwindcss'; @import '@app/design/styles.css';
import { Button, PageHeader, Section } from '@app/design';
```

For client-side navigation, wrap the link components for the router once:

```tsx
import { Link, createLink } from '@tanstack/react-router';
import { ButtonLink, TabBar } from '@app/design';

export const RouterButtonLink = createLink(ButtonLink);
<TabBar items={items} renderLink={({ href, ...props }) => <Link to={href} {...props} />} />;
```

## Commands

From the repository root:

```bash
make design                      # build the previews, then run every check (CI runs this)
pnpm --dir web build:design      # just build: open web/design/dist/previews/index.html via a server
pnpm --dir web check:design      # just check (after a build)
pnpm --dir web exec vitest run design   # the component tests
```

`check:design` fails when:

- a component exported from `src/index.ts` has no `.design-sync/previews/<Name>.tsx`;
- a preview's first line isn't `// @dsCard group="<Group>"`, or its built page's first line isn't
  the matching `<!-- @dsCard group="<Group>" -->` (Claude Design groups its cards by it);
- a colour in `DESIGN.md`'s front matter differs from `tokens.css`;
- a component, preview or app file (`web/src`, `web/index.html`) has a raw colour, a Tailwind
  palette class, a `dark:` variant or an opacity modifier (`bg-primary/50`);
- axe finds a serious or critical WCAG 2.2 A/AA problem on any preview page, or a page throws. Pages
  are checked with the OS in light and in dark mode: OpenCozy has one (light) theme, and it must
  stay readable whatever the OS asks for.

## Browsers

The wall display's Android WebView may be older than current browsers, so:

- Component CSS and the profile classes are **unlayered** plain CSS. An engine without cascade
  layers (before Chrome/WebView 99, Safari 15.4) ignores every `@layer` block, Tailwind's utilities
  and element defaults included, but still draws every component. The cost: a utility in a
  component's `className` can't override a property the component sets, so `className` is for
  placement, not restyling.
- No `color-mix()`: the profile `line` and `track` colours are precomputed in `tokens.css`, and the
  check forbids opacity modifiers (they compile to `color-mix()`, Chrome 111 / Safari 16.4).
- Tokens are hex, not `oklch()`. `100dvh` follows a `100vh` fallback. `ListRow`'s narrow layout uses
  a container query (Chrome 105); without one, rows keep the wide layout, which still works.
- Tailwind utilities that rely on `@property` (gradients, transforms, ring shadows) are best avoided
  on the wall's screens until the WebView version is known.

## Grow it

- **New token**: add it to `tokens.css`, map it in `theme.css` (`--color-<name>: var(--<name>)`),
  add it to `DESIGN.md`'s front matter, and name it in `.design-sync/conventions.md`. Check its
  contrast against the surfaces it sits on (the rules are at the top of `tokens.css`).
- **New component**: `src/components/<Name>.tsx` with its `<name>.css` (tokens only, imported from
  `src/styles.css`), exported from `src/index.ts`; `.design-sync/previews/<Name>.tsx` (first line
  `// @dsCard group="<Group>"`, one named export per state worth seeing: default, disabled, error,
  empty, long text); its props in `conventions.md`; a test in `src/test/` when it has semantics
  worth pinning (roles, names, ARIA wiring). Then `make design`.
- No inline `style`: the app's CSP forbids it. A dynamic value goes through a native element
  (`<progress>`) or an SVG attribute.
- Screens stay in the app (`web/src/routes`). A pattern used on two screens becomes a component
  here.
- Fictional data only in previews: they are public and uploaded to Claude Design.

## Sync with Claude Design

Pushing to claude.ai/design is something you start; agents don't. In Claude Code, from this
folder:

1. `make design` from the repository root, and fix anything it reports.
2. `cd web/design`, start Claude Code, and run `/design-sync`. It reads
   `.design-sync/config.json`, builds with `buildCmd`, and renders the previews into cards.
3. When it lists design-system projects, pick the existing OpenCozy project to update it, or
   **Create new** for a fresh one. It records the project's id in `.design-sync/config.json`. This
   repository is public: commit the id only once you have decided it may be public.
4. Review the plan it shows (the files it will write or delete), approve it, and let it sync. It
   goes one component at a time and never replaces the whole project. Component names and props
   match the previous OpenCozy package, except: tokens have semantic names (`--background`, not
   `--canvas`), `Button` defaults to `secondary`, `PersonTab` lost the `accent` alias, and
   `Textarea`, `ConfirmDialog`, `Countdown` and `QrCode` are new.
5. Commit what it changed under `.design-sync/` (it may add `NOTES.md` with what it learned about
   rendering this package). Its caches (`.ds-sync/`, `ds-bundle/`, `.design-sync/.cache/`) are
   git-ignored.

Re-run `/design-sync` after changing a component, its preview or the tokens. A change only to a
component's styling keeps the card's previous review grade, so look at its new card before you
trust it.
