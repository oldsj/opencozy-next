// @dsCard group="Layout"
import { pointsLabel, Button, ButtonLink, CheckCircle, ConfirmDialog, ChoiceGroup, Input, Notice, SegmentedControl, Tag, List, ListRow, ManageNav, ManageShell, PageHeader, ProfileAvatar, Section, Sheet, Textarea } from '../../src';
import {
  calendarErrorText,
  calendars,
  clock,
  eventDetail,
  kids,
  providerLabels,
  type CalendarSource,
} from './fixtures-calendar';
import {
  children as householdChildren,
  credentials,
  issued,
  kindLabels as credentialKinds,
  scopeLabels,
  type Credential,
} from './fixtures-credentials';
import { inbox, kindLabels, REJECT_REASON_MAX, type InboxItem } from './fixtures-inbox';
import {
  dayLabels,
  INGREDIENT_MAX,
  MEAL_NOTE_MAX,
  mealSlots,
  recipe,
  RECIPE_TITLE_MAX,
  recipes,
  slotLabels,
  STEP_MAX,
  week,
} from './fixtures-meals';
import { manageGroups } from './fixtures-manage';

export function Chores() {
  return (
    <ManageShell
      nav={<ManageNav groups={manageGroups('/manage/chores')} shortcut="/manage/inbox" back={{ href: '/today', label: 'Back to family' }} />}
    >
      <PageHeader title="Chores" description="What each child can do for points." actions={<Button variant="primary">Add chore</Button>} />
      <Section title="Every day">
        <List>
          <ListRow title="Feed the cat" meta="Ezra · 4 points · needs approval" trailing={<Button variant="quiet">Edit</Button>} />
          <ListRow title="Tidy the hallway" meta="Ada, Noor · 5 points" trailing={<Button variant="quiet">Edit</Button>} />
        </List>
      </Section>
    </ManageShell>
  );
}

const noop = () => undefined;

function InboxRow({ item, saving = false }: { item: InboxItem; saving?: boolean }) {
  const meta = [item.child.name, item.points === undefined ? null : pointsLabel(item.points), kindLabels[item.kind], item.submittedAt]
    .filter(Boolean)
    .join(' · ');
  const leading = <ProfileAvatar name={item.child.name} color={item.child.color} size="small" aria-hidden="true" />;
  if (item.kind === 'time_request') {
    return (
      <ListRow
        stackOnNarrow
        leading={leading}
        title={`${item.child.name} wants more iPad time`}
        meta={meta}
        trailing={
          <ChoiceGroup
            label={`More iPad time for ${item.child.name}`}
            onChoose={noop}
            choices={[
              { value: '30', label: '30 min' },
              { value: '60', label: '1 hour' },
              { value: 'all_day', label: 'All day' },
              { value: 'no', label: 'No', variant: 'quiet' },
            ]}
          />
        }
      />
    );
  }
  return (
    <ListRow
      stackOnNarrow
      leading={leading}
      title={item.title}
      meta={meta}
      trailing={
        <>
          <Button variant="danger" disabled={saving} aria-label={`Send back: ${item.title} (${item.child.name})`}>
            Send back…
          </Button>
          <Button variant="primary" disabled={saving} aria-label={saving ? undefined : `Approve: ${item.title} (${item.child.name})`}>
            {saving ? 'Saving…' : 'Approve'}
          </Button>
        </>
      }
    />
  );
}

/** The adult inbox (phone and desktop), built only from `inbox` (fixtures-inbox.ts). */
export function Inbox() {
  return (
    <ManageShell
      nav={<ManageNav groups={manageGroups('/manage/inbox')} shortcut="/manage/inbox" back={{ href: '/today', label: 'Back to family' }} />}
    >
      <PageHeader title="Inbox" description="Approve what the children finished. Oldest first." />
      <Section title="Waiting for you" count={inbox.items.length}>
        <List>
          {inbox.items.map((item, i) => (
            <InboxRow key={item.id} item={item} saving={i === 0} />
          ))}
        </List>
      </Section>
    </ManageShell>
  );
}

/** Sending a chore back: the reason is optional, shown to the child, up to 200 characters. */
export function InboxSendBack() {
  return (
    <>
      <Inbox />
      <Sheet
        open
        onClose={noop}
        title="Send back Tidy the hallway?"
        description="Ada keeps the chore and can tick it again. No points move."
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="danger">Send back</Button>
          </>
        }
      >
        <Textarea
          label="Reason for Ada (optional)"
          hint={`Shown on Ada’s list. Up to ${String(REJECT_REASON_MAX)} characters.`}
          maxLength={REJECT_REASON_MAX}
          defaultValue="The shoes are tidied, but the coats still need hanging up."
        />
      </Sheet>
    </>
  );
}

const mealsNav = (current: string) => (
  <ManageNav groups={manageGroups(current)} shortcut="/manage/inbox" back={{ href: '/today', label: 'Back to family' }} />
);

/** The week's meal plan for adults: every day, every slot, empty slots ready to fill. */
export function MealPlan() {
  return (
    <ManageShell nav={mealsNav('/manage/meals')}>
      <PageHeader
        title="Meals this week"
        description="Week of Monday, Sep 14."
        actions={
          <>
            <Button>Previous week</Button>
            <Button>Next week</Button>
          </>
        }
      />
      {week.days.map((day) => (
        <Section key={day.date} title={dayLabels[day.date] ?? day.date}>
          <List>
            {mealSlots.map((slot) => {
              const meal = day.meals.find((m) => m.slot === slot);
              const label = `${slotLabels[slot]}, ${dayLabels[day.date] ?? day.date}`;
              return (
                <ListRow
                  key={slot}
                  stackOnNarrow
                  muted={meal === undefined}
                  title={meal?.title ?? 'Nothing planned'}
                  meta={[slotLabels[slot], meal?.note].filter(Boolean).join(' · ')}
                  trailing={
                    <Button variant="quiet" aria-label={meal ? `Change ${label}` : `Plan ${label}`}>
                      {meal ? 'Change' : 'Plan'}
                    </Button>
                  }
                />
              );
            })}
          </List>
        </Section>
      ))}
    </ManageShell>
  );
}

/** Planning one slot: pick a recipe or write a plain entry such as "Leftovers". */
export function MealPlanSlot() {
  return (
    <>
      <MealPlan />
      <Sheet
        open
        onClose={noop}
        title="Plan Thursday’s dinner"
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="primary">Save</Button>
          </>
        }
      >
        <div className="grid gap-4">
          <Input
            label="Meal"
            hint="Type a recipe’s name to use it, or anything else such as Leftovers."
            defaultValue="Lentil soup with crusty bread"
            maxLength={RECIPE_TITLE_MAX}
            list="recipe-suggestions"
            autoComplete="off"
          />
          <datalist id="recipe-suggestions">
            {recipes
              .filter((r) => !r.archived)
              .map((r) => (
                <option key={r.id} value={r.title} />
              ))}
          </datalist>
          <Textarea label="Note (optional)" hint={`Up to ${String(MEAL_NOTE_MAX)} characters.`} maxLength={MEAL_NOTE_MAX} defaultValue="Use the second batch from Monday." />
        </div>
      </Sheet>
    </>
  );
}

/** The recipe list; long titles wrap. */
export function Recipes() {
  return (
    <ManageShell nav={mealsNav('/manage/recipes')}>
      <PageHeader title="Recipes" description="What the family cooks." actions={<Button variant="primary">Add recipe</Button>} />
      <Section title="All recipes" count={recipes.filter((r) => !r.archived).length}>
        <List>
          {recipes.map((r) => (
            <ListRow
              key={r.id}
              stackOnNarrow
              muted={r.archived}
              title={r.title}
              meta={r.archived ? `Serves ${String(r.servings)} · archived, kept on past meals` : `Serves ${String(r.servings)}`}
              trailing={
                <ButtonLink variant="quiet" href={`#recipe-${r.id}`} aria-label={`Open ${r.title}`}>
                  Open
                </ButtonLink>
              }
            />
          ))}
        </List>
      </Section>
    </ManageShell>
  );
}

/** Editing a recipe: ingredients one per line, each step its own multi-line field. */
export function RecipeEdit() {
  return (
    <ManageShell nav={mealsNav('/manage/recipes')}>
      <PageHeader title="Edit recipe" actions={<Button variant="primary">Save recipe</Button>} />
      <form className="grid max-w-3xl gap-6">
        <Input label="Name" defaultValue={recipe.title} maxLength={RECIPE_TITLE_MAX} required />
        <Input label="Serves" type="number" inputMode="numeric" min={1} max={100} defaultValue={recipe.servings} required />
        <Textarea
          label="Ingredients"
          hint={`One per line, up to 40, each up to ${String(INGREDIENT_MAX)} characters.`}
          rows={8}
          defaultValue={recipe.ingredients.join('\n')}
        />
        <Section title="Steps" description="Up to 40. Each step can have several lines.">
          <ol className="grid gap-4">
            {recipe.steps.map((step, i) => (
              // Steps have no ids: key by position, as the API returns them.
              <li key={i} className="grid gap-2">
                <Textarea label={`Step ${String(i + 1)}`} maxLength={STEP_MAX} rows={3} defaultValue={step} />
                <div>
                  <Button variant="quiet" aria-label={`Remove step ${String(i + 1)}`}>
                    Remove
                  </Button>
                </div>
              </li>
            ))}
          </ol>
        </Section>
        <div>
          <Button>Add a step</Button>
        </div>
      </form>
    </ManageShell>
  );
}

/** An event for adults, with its description, and who it's for. */
export function EventDetail({ assigning = false }: { assigning?: boolean }) {
  const e = eventDetail;
  return (
    <ManageShell nav={mealsNav('/manage/calendar')}>
      <PageHeader
        title={e.title ?? 'Busy'}
        description={`Monday, Sep 14 · ${clock(e.start)} – ${clock(e.end)} · ${e.calendar.name}${e.recurring ? ' · repeats' : ''}`}
      />
      {e.location ? <p>{e.location}</p> : null}
      {e.description ? <p className="whitespace-pre-line">{e.description}</p> : null}
      <Section title="For" description="Shown on their cards and the wall display.">
        <List>
          {Object.entries(kids).map(([id, kid]) => (
            <ListRow
              key={id}
              leading={<ProfileAvatar name={kid.name} color={kid.color} size="small" aria-hidden="true" />}
              title={kid.name}
              trailing={<CheckCircle checked={e.children.includes(id)} label={`${e.title ?? 'Busy'} is for ${kid.name}`} disabled={!assigning} />}
            />
          ))}
        </List>
        {assigning ? null : (
          <div className="mt-4">
            <Button>Change who it’s for</Button>
          </div>
        )}
      </Section>
    </ManageShell>
  );
}

/** Saving who a repeating event is for: this time, or every time. */
export function EventAssign() {
  return (
    <>
      <EventDetail assigning />
      <Sheet
        open
        onClose={noop}
        title="Save who Swimming lesson is for?"
        description="Swimming lesson repeats every Monday and Wednesday."
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="primary">Save</Button>
          </>
        }
      >
        <SegmentedControl
          label="Apply to"
          value="series"
          onChange={noop}
          options={[
            { value: 'occurrence', label: 'Just this one' },
            { value: 'series', label: 'Every time' },
          ]}
        />
      </Sheet>
    </>
  );
}

function CalendarRow({ source }: { source: CalendarSource }) {
  const synced = source.lastSyncAt === undefined ? 'Not synced yet' : `Synced ${source.lastSyncAt}`;
  const meta = [providerLabels[source.provider], source.showOnDisplay ? 'On the wall display' : 'Adults only', synced].join(' · ');
  return (
    <ListRow
      stackOnNarrow
      leading={<ProfileAvatar name={source.name} color={source.color} size="small" aria-hidden="true" />}
      title={source.name}
      meta={
        <>
          {meta}
          {source.error ? <span className="mt-1 block">{calendarErrorText[source.error]}</span> : null}
        </>
      }
      trailing={
        <>
          {source.status === 'error' ? <Tag tone="error">Not updating</Tag> : null}
          {source.status === 'needs_reauth' ? (
            <Button variant="primary" aria-label={`Sign in again: ${source.name}`}>
              Sign in again
            </Button>
          ) : null}
          <Button variant="quiet" aria-label={`Edit ${source.name}`}>
            Edit
          </Button>
        </>
      }
    />
  );
}

/** Calendar settings: each source, its sync state and a sentence for any error; never the feed URL. */
export function Calendars() {
  return (
    <ManageShell nav={mealsNav('/manage/calendars')}>
      <PageHeader title="Calendars" description="Choose which ones show on the wall display." />
      <Notice tone="attention" title="2 calendars need attention">
        School hasn’t updated since yesterday, and Sports needs you to sign in again.
      </Notice>
      <Section title="Connected" count={calendars.length}>
        <List>
          {calendars.map((source) => (
            <CalendarRow key={source.id} source={source} />
          ))}
        </List>
      </Section>
      <Section title="Add a calendar">
        <div className="flex flex-wrap gap-3">
          <Button>Add a calendar link</Button>
          <Button>Connect Google</Button>
          <Button>Connect iCloud</Button>
        </div>
      </Section>
    </ManageShell>
  );
}

/** Adding an ICS link: the address is write-only and shows only as set afterwards. */
export function CalendarAddLink() {
  return (
    <>
      <Calendars />
      <Sheet
        open
        onClose={noop}
        title="Add a calendar link"
        description="Paste the private address from the calendar’s sharing settings."
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="primary">Add calendar</Button>
          </>
        }
      >
        <div className="grid gap-4">
          <Input label="Name" maxLength={80} defaultValue="School" />
          <Input label="Calendar address" type="url" inputMode="url" hint="Starts with https:// or webcal://. OpenCozy never shows it again." placeholder="https://calendar.example.test/family.ics" />
        </div>
      </Sheet>
    </>
  );
}

const settingsNav = (current: string) => mealsNav(current);

function whoFor(c: Credential): string {
  if (c.allChildren) return 'All children';
  return c.childIds.map((id) => householdChildren.find((k) => k.id === id)?.name ?? id).join(', ');
}

/** Service and assistant credentials: what each can see, for whom; revoking stops it at once. */
export function Credentials() {
  return (
    <ManageShell nav={settingsNav('/manage/credentials')}>
      <PageHeader
        title="Connected services"
        description="Home servers and assistants that can read the family’s day. Each has its own key."
        actions={<Button variant="primary">Add a service</Button>}
      />
      <Section title="Connected" count={credentials.length}>
        <List>
          {credentials.map((c) => (
            <ListRow
              key={c.id}
              stackOnNarrow
              title={c.name}
              meta={[
                credentialKinds[c.kind].label,
                c.scopes.map((s) => scopeLabels[s] ?? s).join('; '),
                whoFor(c),
                `added ${c.createdAt}`,
                c.expiresAt === undefined ? null : `expires ${c.expiresAt}`,
              ]
                .filter(Boolean)
                .join(' · ')}
              trailing={
                <Button variant="danger" aria-label={`Revoke ${c.name}`}>
                  Revoke…
                </Button>
              }
            />
          ))}
        </List>
      </Section>
    </ManageShell>
  );
}

/** Issuing a credential: what kind, a name, what it may see and for which children. */
export function CredentialIssue() {
  return (
    <>
      <Credentials />
      <Sheet
        open
        onClose={noop}
        title="Add a service"
        description="You’ll see its key once, on the next screen."
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="primary">Create key</Button>
          </>
        }
      >
        <div className="grid gap-5">
          <SegmentedControl
            label="Kind"
            value="service"
            onChange={noop}
            options={[
              { value: 'service', label: credentialKinds.service.label },
              { value: 'assistant', label: credentialKinds.assistant.label },
            ]}
          />
          <p className="m-0 text-muted-foreground">{credentialKinds.service.detail}</p>
          <Input label="Name" hint="So you recognise it later, such as Router." maxLength={80} defaultValue="Router" required />
          <Section title="It can">
            <List>
              {Object.entries(scopeLabels).map(([scope, label]) => (
                <ListRow key={scope} title={label} meta={scope} trailing={<CheckCircle checked={scope === 'children:read'} label={label} />} />
              ))}
            </List>
          </Section>
          <SegmentedControl
            label="For"
            value="all"
            onChange={noop}
            options={[
              { value: 'all', label: 'All children' },
              { value: 'some', label: 'Choose children' },
            ]}
          />
        </div>
      </Sheet>
    </>
  );
}

/** The key, shown once. A retry of the same request never shows it again. */
export function CredentialSecret() {
  return (
    <>
      <Credentials />
      <Sheet
        open
        onClose={noop}
        title={`Key for ${issued.credential.name}`}
        description="Copy it into the service now. OpenCozy won’t show it again; if you lose it, revoke this one and add another."
        footer={<Button variant="primary">Done</Button>}
      >
        <div className="grid gap-3">
          <Input label="Key" readOnly value={issued.secret} className="font-mono" />
          <div>
            <Button>Copy key</Button>
          </div>
        </div>
      </Sheet>
    </>
  );
}

export function CredentialRevoke() {
  return (
    <>
      <Credentials />
      <ConfirmDialog
        open
        onCancel={noop}
        onConfirm={noop}
        title="Revoke Kitchen assistant?"
        description="It stops working at once. To connect it again, add it as a new service."
        confirmLabel="Revoke"
        tone="danger"
      />
    </>
  );
}

/** Children, with archived ones kept apart; archiving keeps their history, restoring brings them back. */
export function Children() {
  const active = householdChildren.filter((c) => !c.archived);
  const archived = householdChildren.filter((c) => c.archived);
  return (
    <ManageShell nav={settingsNav('/manage/children')}>
      <PageHeader title="Children" actions={<Button variant="primary">Add a child</Button>} />
      <Section title="Children" count={active.length}>
        <List>
          {active.map((c) => (
            <ListRow
              key={c.id}
              leading={<ProfileAvatar name={c.name} color={c.color} size="small" aria-hidden="true" />}
              title={c.name}
              trailing={
                <>
                  <Button variant="quiet" aria-label={`Rename ${c.name}`}>
                    Rename
                  </Button>
                  <Button variant="quiet" aria-label={`Archive ${c.name}`}>
                    Archive…
                  </Button>
                </>
              }
            />
          ))}
        </List>
      </Section>
      <Section title="Archived" count={archived.length} description="Off every screen, with their points and history kept.">
        <List>
          {archived.map((c) => (
            <ListRow
              key={c.id}
              muted
              leading={<ProfileAvatar name={c.name} color={c.color} size="small" aria-hidden="true" />}
              title={c.name}
              trailing={
                <Button aria-label={`Restore ${c.name}`}>
                  Restore
                </Button>
              }
            />
          ))}
        </List>
      </Section>
    </ManageShell>
  );
}

export function ChildArchive() {
  return (
    <>
      <Children />
      <ConfirmDialog
        open
        onCancel={noop}
        onConfirm={noop}
        title="Archive Noor?"
        description="Noor leaves the wall display, the inbox and every list. Their points and history are kept, and you can restore them any time. Anything waiting for approval stays waiting until then."
        confirmLabel="Archive"
      />
    </>
  );
}
