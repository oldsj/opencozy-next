## Using @app/design (OpenCozy)

No provider or wrapper is needed: import the components and render them. `styles.css` (loaded by
the design tool) holds every token, both fonts and every class the components use.

### Components

Build screens from these rather than hand-rolling lookalikes; if a screen needs something new, it
becomes a component here first. Every component's `className` extends its classes (use it for
placement such as margins or grid areas, not restyling).

- **Actions.** `Button` takes `variant="primary" | "secondary" | "quiet" | "danger"` (default
  `secondary`; one `primary` per view; `quiet` for repeated row actions like Edit; `danger` to
  cancel, archive or end, never a filled red block) and `fullWidth`. `ButtonPrimary` and
  `ButtonSecondary` fix the variant. `ButtonLink` is an `<a>` with the same look.
- **Forms.** `Input` and `Textarea` take a visible `label`, `hint` and `error` (marks the control
  invalid and is announced). `Field` takes `label`, `htmlFor`, `hint` and one native input, select
  or textarea. `SegmentedControl` takes `label`, `options` (`{ value, label }`), `value`,
  `onChange`, for two to five views of one section.
- **Layout.** `AppFrame` (`topBar`, `tabBar`, content: skip link and main landmark), `TopBar`
  (`time`, `period`, `date`, `householdName`, `status`, `actions`), `TabBar` (items with `href`,
  `label`, a Lucide `icon`, `current`; never wraps, 64px targets), `PageHeader` (`title`, the page's
  one h1; `description`; `actions`), `Section` (`title` as an h2, `count`, `description`,
  `actions`), `Panel` (`heading`, `description`, `tone`, `as`), `List` with `ListRow` (`title`,
  `meta`, `leading`, `trailing`, `muted`, `stackOnNarrow`): lists, never cards in cards.
- **Overlays.** `Sheet` (`open`, `onClose`, `title`, `description`, `footer`, `placement`) for forms;
  `ConfirmDialog` (`title` as a question, `confirmLabel`, `tone="danger"`, `pending`, `error`,
  `onConfirm`, `onCancel`) before anything destructive.
- **Feedback.** `Notice` (`tone="info" | "success" | "attention" | "error"`, `title`, `actions`;
  errors are alerts), `Toast` (a brief fixed confirmation), `Tag` (only the exceptional state),
  `StatusPill` (`live` renders nothing), `EmptyState` (`title`, `description`, `icon`, `action`,
  `variant`), `ComingLaterTile` (planned features, never interactive).
- **People.** `PersonTab` (`name`, `points`, `selected`, `color`; inside a `role="tablist"`),
  `ProfileAvatar` (`name`, `color`, `size`), `PointsBadge` (`points`), `ProgressBar` (`value`,
  `max`, an accessible-only `label`). `color` is one of `rose`, `sage`, `sky`, `butter`, `lilac`,
  `peach`, `teal`, `sand`, and is always shown with the name.
- **Today and rewards.** `PersonCard` (`name`, `color`, `points`, `completed`, `total`,
  `earnedToday`, `footer`; `TaskRow` children), `TaskRow` (`title`, `assignee`, `points`, `metadata`,
  `status`, `onAction` or `action`, or `check` for the 56px `CheckCircle`; `error`, `celebrate`),
  `RewardTile` (`name`, `cost`, `balance`, `onSelect`).
- **Screen time and pairing.** `Countdown` (`seconds` left, `size="inline" | "large"`: "24:59 left",
  announced in whole minutes), `QrCode` (`modules`, the QR matrix as rows of booleans; `label`).

### Tokens

Layout around the components uses token utilities: colours `bg-background`, `bg-surface`,
`bg-surface-soft`, `bg-surface-muted`, `text-foreground`, `text-muted-foreground`, `border-border`,
`border-input`, `bg-primary`, `text-primary-foreground`, and each lane's `positive`, `attention`,
`danger`, `info` with `-ink`, `-soft`, `-line`; type `font-heading`, `font-body`, `text-clock`,
`text-page`, `text-section`, `text-task`, `text-body`, `text-meta`, `text-label`; shape
`rounded-panel`, `rounded-control`, `rounded-small`, `rounded-pill`, `shadow-soft`; spacing is the
4px scale (`gap-3` = 12px). In plain CSS, the variables (`var(--surface)`). There is no Tailwind
palette, no default type scale and no dark mode. Never write a hex or `rgb()` colour, an inline
`style`, or an opacity modifier such as `bg-primary/50`.

### Accessibility

Touch targets are at least 48px (64px for family navigation and child actions, 56px for the task
check). Focus is a 3px `ring` outline offset by 3px. Colour never carries identity or state alone.
Icons come from `lucide-react` (named imports, default stroke) and stay paired with a label.

## Today (slice one)

- A person's column is a `PersonCard` inside a `PeopleBoard`; routines and chores are `TaskGroup`s of `TaskRow`s. Rows take the person's `--profile-wash`.
- Show stale or offline data with a `ConnectionBanner` above the content; the TopBar's `StatusPill` only flags it.
- Display pairing uses `CodeInput` (six characters, no autocomplete).
