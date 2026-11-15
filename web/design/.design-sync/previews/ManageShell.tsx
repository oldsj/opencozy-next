// @dsCard group="Layout"
import { pointsLabel, Button, ButtonLink, CheckCircle, ConfirmDialog, DayPicker, Countdown, QrCode, qrModulesFromRows, ChoiceGroup, Input, Notice, SegmentedControl, Tag, List, ListRow, ManageNav, ManageShell, PageHeader, ProfileAvatar, Section, Sheet, Textarea } from '../../src';
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
import { devices, offer, platformLabels, type Device } from './fixtures-devices';
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
import { awardRules, learningErrorText, learningLinks, providerNames, type AwardRule } from './fixtures-learning';
import { adaHours, DEFAULT_LOCK_MESSAGE, LOCK_MESSAGE_MAX, webFilter } from './fixtures-screentime';
import { me, oidcLink, passkeys, phoneSessions, type OidcLink } from './fixtures-account';
import {
  catalogErrorText,
  catalogRewards,
  chores,
  daysLabel,
  household,
  kids as catalogKids,
  redemptions,
  refundFor,
  routines,
  timeZones,
  type RedemptionRow,
} from './fixtures-catalog';
import { manageGroups } from './fixtures-manage';
import { durationLabels, tvs, type Tv } from './fixtures-tv';

/** Chores by child: what each is worth and on which days. Edit changes or reassigns; archive keeps history. */
export function Chores() {
  return (
    <ManageShell
      nav={<ManageNav groups={manageGroups('/manage/chores')} shortcut="/manage/inbox" back={{ href: '/today', label: 'Back to family' }} />}
    >
      <PageHeader title="Chores" description="What each child can do for points." actions={<Button variant="primary">Add chore</Button>} />
      {catalogKids.map((kid) => {
        const mine = chores.filter((c) => c.childId === kid.id);
        if (mine.length === 0) return null;
        return (
          <Section key={kid.id} title={kid.name} count={mine.length}>
            <List>
              {mine.map((c) => (
                <ListRow
                  key={c.id}
                  stackOnNarrow
                  leading={<ProfileAvatar name={kid.name} color={kid.color} size="small" aria-hidden="true" />}
                  title={c.title}
                  meta={`${pointsLabel(c.points)} · ${daysLabel(c.days)}`}
                  trailing={
                    <>
                      <Button variant="quiet" aria-label={`Edit ${c.title}`}>
                        Edit
                      </Button>
                      <Button variant="quiet" aria-label={`Archive ${c.title}`}>
                        Archive…
                      </Button>
                    </>
                  }
                />
              ))}
            </List>
          </Section>
        );
      })}
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
        title="Connections"
        description="Home servers, assistants and learning services. Each has its own key and sees only what you allow."
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
                c.awardRuleIds.length > 0 ? `awards ${c.awardRuleIds.map((id) => awardRules.find((r) => r.id === id)?.activityKey ?? id).join(', ')}` : null,
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

/**
 * Issuing a credential: what kind, a name, what it may do and for which children. Award rules
 * appear only when "Give points for learning" is picked. Needs a sign-in within 10 minutes.
 */
export function CredentialIssue({ learning = false, signIn = false }: { learning?: boolean; signIn?: boolean }) {
  const picked = learning ? ['learning:award'] : ['children:read'];
  const live = awardRules.filter((r) => !r.archived);
  return (
    <>
      <Credentials />
      <Sheet
        open
        onClose={noop}
        title="Add a connection"
        description="You’ll see its key once, on the next screen."
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="primary" disabled={signIn}>
              Create key
            </Button>
          </>
        }
      >
        <div className="grid gap-5">
          {signIn && (
            <Notice tone="attention" title="Sign in again first">
              {learningErrorText.recent_auth_required} <ButtonLink href="#sign-in">Sign in again</ButtonLink>
            </Notice>
          )}
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
          <Input label="Name" hint="So you recognise it later, such as Router." maxLength={80} defaultValue={learning ? 'Minecraft server' : 'Router'} required />
          <Section title="It can">
            <List>
              {Object.entries(scopeLabels).map(([scope, label]) => (
                <ListRow key={scope} title={label} meta={scope} trailing={<CheckCircle checked={picked.includes(scope)} label={label} />} />
              ))}
            </List>
          </Section>
          {learning && (
            <Section title="Award rules it can use" description="It can give points only by these rules, up to each rule’s daily limit.">
              <List>
                {live.map((r) => (
                  <ListRow key={r.id} title={r.activityKey} meta={ruleMeta(r)} trailing={<CheckCircle checked label={`Use ${r.activityKey}`} />} />
                ))}
              </List>
            </Section>
          )}
          <SegmentedControl
            label="For"
            value={learning ? 'some' : 'all'}
            onChange={noop}
            options={[
              { value: 'all', label: 'All children' },
              { value: 'some', label: 'Choose children' },
            ]}
          />
          {learning && (
            <List>
              {householdChildren
                .filter((c) => !c.archived)
                .map((c) => (
                  <ListRow
                    key={c.id}
                    leading={<ProfileAvatar name={c.name} color={c.color} size="small" aria-hidden="true" />}
                    title={c.name}
                    trailing={<CheckCircle checked={c.id !== 'c3'} label={c.name} />}
                  />
                ))}
            </List>
          )}
        </div>
      </Sheet>
    </>
  );
}

/** A learning service: points by award rules, for chosen children. */
export function CredentialIssueLearning() {
  return <CredentialIssue learning />;
}

/** The last sign-in is more than 10 minutes old (recent_auth_required): sign in again, then create. */
export function CredentialIssueSignIn() {
  return <CredentialIssue signIn />;
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

function DeviceRow({ device }: { device: Device }) {
  const meta = [
    platformLabels[device.platform],
    device.child ? `${device.child.name}’s` : 'Not assigned to a child',
    `paired ${device.pairedAt}`,
    device.lastSeenAt === undefined ? 'not seen yet' : `seen ${device.lastSeenAt}`,
  ].join(' · ');
  return (
    <ListRow
      stackOnNarrow
      muted={device.child === null}
      leading={device.child ? <ProfileAvatar name={device.child.name} color={device.child.color} size="small" aria-hidden="true" /> : undefined}
      title={device.name}
      meta={meta}
      trailing={
        <>
          <Button variant="quiet" aria-label={`Rename ${device.name}`}>
            Rename
          </Button>
          <Button variant="quiet" aria-label={`Change whose ${device.name} is`}>
            Change child
          </Button>
          <Button variant="danger" aria-label={`Unpair ${device.name}`}>
            Unpair…
          </Button>
        </>
      }
    />
  );
}

/** Children's devices: whose each is and when it was last seen. Pairing starts here, per child. */
export function Devices() {
  return (
    <ManageShell nav={settingsNav('/manage/devices')}>
      <PageHeader
        title="Devices"
        description="The children’s iPads and phones. Each shows only its own child’s day."
        actions={<Button variant="primary">Pair a device</Button>}
      />
      <Section title="Paired" count={devices.devices.length}>
        <List>
          {devices.devices.map((d) => (
            <DeviceRow key={d.id} device={d} />
          ))}
        </List>
      </Section>
    </ManageShell>
  );
}

/**
 * Pairing a device for Ada: the code and QR stay up, with the time left, while the page waits for
 * the device. On the iPad, the OpenCozy app scans the QR or takes the code.
 */
export function DevicePair() {
  return (
    <>
      <Devices />
      <Sheet
        open
        onClose={noop}
        title="Pair a device for Ada"
        description="On Ada’s iPad, open the OpenCozy app and scan this code, or type it in."
        footer={<Button>Cancel</Button>}
      >
        <div className="grid justify-items-center gap-5 text-center">
          <div className="w-60 max-w-full">
            <QrCode modules={qrModulesFromRows(offer.qrModules)} label="Pairing code for Ada’s iPad" />
          </div>
          <p className="m-0 text-page font-semibold tabular-nums tracking-widest" aria-label={`Code ${offer.code.split('').join(' ')}`}>
            {offer.code}
          </p>
          <p className="m-0 text-muted-foreground">
            Waiting for the iPad · <Countdown seconds={562} lowAt={60} />
          </p>
        </div>
      </Sheet>
    </>
  );
}

export function DeviceReassign() {
  return (
    <>
      <Devices />
      <Sheet
        open
        onClose={noop}
        title="Whose is Old iPhone?"
        description="It switches to that child’s day straight away; no need to pair again."
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="primary">Save</Button>
          </>
        }
      >
        <ChoiceGroup
          label="Child for Old iPhone"
          onChoose={noop}
          choices={[
            { value: 'c1', label: 'Ada' },
            { value: 'c2', label: 'Ezra' },
            { value: 'c3', label: 'Noor' },
          ]}
        />
      </Sheet>
    </>
  );
}

export function DeviceUnpair() {
  return (
    <>
      <Devices />
      <ConfirmDialog
        open
        onCancel={noop}
        onConfirm={noop}
        title="Unpair Ezra’s iPad?"
        description="It signs out at once and shows the pairing screen. Ezra’s points and history stay."
        confirmLabel="Unpair Ezra’s iPad"
        tone="danger"
      />
    </>
  );
}

function ruleMeta(r: AwardRule): string {
  return `${pointsLabel(r.points)} each · up to ${pointsLabel(r.dailyLimit)} a day`;
}

/**
 * Learning: the award rules a learning service may use, and which player is which child. A service
 * gives points only for a linked child, by a live rule, under its daily limit.
 */
export function Learning() {
  const live = awardRules.filter((r) => !r.archived);
  const archived = awardRules.filter((r) => r.archived);
  return (
    <ManageShell nav={settingsNav('/manage/learning')}>
      <PageHeader title="Learning" description="Points for learning: what each activity is worth, and which player is which child." />
      <Section title="Award rules" count={live.length} actions={<Button variant="primary">Add a rule</Button>}>
        <List>
          {live.map((r) => (
            <ListRow
              key={r.id}
              stackOnNarrow
              title={r.activityKey}
              meta={ruleMeta(r)}
              trailing={
                <>
                  <Button variant="quiet" aria-label={`Edit ${r.activityKey}`}>
                    Edit
                  </Button>
                  <Button variant="quiet" aria-label={`Archive ${r.activityKey}`}>
                    Archive…
                  </Button>
                </>
              }
            />
          ))}
        </List>
      </Section>
      <Section title="Archived rules" count={archived.length} description="No longer awarded. Points already given stay.">
        <List>
          {archived.map((r) => (
            <ListRow key={r.id} muted title={r.activityKey} meta={ruleMeta(r)} />
          ))}
        </List>
      </Section>
      <Section title="Players" count={learningLinks.length} actions={<Button>Link a player</Button>}>
        <List>
          {learningLinks.map((l) => {
            const child = householdChildren.find((c) => c.id === l.childId);
            const name = child?.name ?? 'A child';
            return (
              <ListRow
                key={l.id}
                stackOnNarrow
                leading={child ? <ProfileAvatar name={child.name} color={child.color} size="small" aria-hidden="true" /> : undefined}
                title={`${name} is ${l.externalId}`}
                meta={`${providerNames[l.provider]} · linked ${l.linkedAt}`}
                trailing={
                  <Button variant="quiet" aria-label={`Unlink ${l.externalId} from ${name}`}>
                    Unlink…
                  </Button>
                }
              />
            );
          })}
        </List>
      </Section>
    </ManageShell>
  );
}

/** Adding or editing a rule; a second live rule for the same activity is rule_exists. */
export function AwardRuleEdit() {
  return (
    <>
      <Learning />
      <Sheet
        open
        onClose={noop}
        title="Add a rule"
        description="The activity key is what the learning service sends when a child does it."
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="primary">Save rule</Button>
          </>
        }
      >
        <div className="grid gap-5">
          <Input label="Activity key" defaultValue="quest.complete" maxLength={64} className="font-mono" error={learningErrorText.rule_exists} />
          <div className="grid grid-cols-2 gap-4">
            <Input label="Points each time" type="number" inputMode="numeric" min={1} defaultValue={5} />
            <Input label="Daily limit, in points" type="number" inputMode="numeric" min={1} defaultValue={20} hint="Per child, per day." />
          </div>
        </div>
      </Sheet>
    </>
  );
}

export function AwardRuleArchive() {
  return (
    <>
      <Learning />
      <ConfirmDialog
        open
        onCancel={noop}
        onConfirm={noop}
        title="Archive lesson.finish?"
        description="Services stop giving points for it. Points already given stay."
        confirmLabel="Archive lesson.finish"
        tone="danger"
      />
    </>
  );
}

/** Linking a player to a child; a player already linked is link_exists. */
export function LearnerLink() {
  return (
    <>
      <Learning />
      <Sheet
        open
        onClose={noop}
        title="Link a player"
        description="Points for this player go to the child you pick."
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="primary">Link</Button>
          </>
        }
      >
        <div className="grid gap-5">
          <ChoiceGroup
            label="Child"
            onChoose={noop}
            choices={householdChildren.filter((c) => !c.archived).map((c) => ({ value: c.id, label: c.name }))}
          />
          <Input label="Minecraft player ID" defaultValue="AdaBuilds" maxLength={64} error={learningErrorText.link_exists} />
        </div>
      </Sheet>
    </>
  );
}

export function LearnerUnlink() {
  return (
    <>
      <Learning />
      <ConfirmDialog
        open
        onCancel={noop}
        onConfirm={noop}
        title="Unlink EzraCrafts from Ezra?"
        description="That player stops earning points for Ezra. Points already given stay."
        confirmLabel="Unlink EzraCrafts"
        tone="danger"
      />
    </>
  );
}

/** Slice 8: a child's allowed hours. They limit when saved iPad time is used, never when it is earned. */
export function ChildHours() {
  return (
    <>
      <Children />
      <Sheet
        open
        onClose={noop}
        title="Ada’s screen time hours"
        description="When Ada can use saved iPad time or start TV time. Earning and saving work at any hour; a parent can always give time."
        footer={
          <>
            <Button variant="quiet">No limit</Button>
            <Button>Cancel</Button>
            <Button variant="primary">Save</Button>
          </>
        }
      >
        <div className="grid gap-6">
          <Section title="School nights" description="Sunday to Thursday">
            <div className="grid grid-cols-2 gap-4">
              <Input type="time" label="From" defaultValue={adaHours.schoolNight.start} />
              <Input type="time" label="Until" defaultValue={adaHours.schoolNight.end} />
            </div>
          </Section>
          <Section title="Weekend" description="Friday and Saturday">
            <div className="grid grid-cols-2 gap-4">
              <Input type="time" label="From" defaultValue={adaHours.weekend.start} />
              <Input type="time" label="Until" defaultValue="07:00" error="Weekend hours must end after they start, on the same day." />
            </div>
          </Section>
        </div>
      </Sheet>
    </>
  );
}

/** A parent gives time for free: iPad time is saved (or started now), TV time adds to the balance. */
export function GiveTime() {
  return (
    <>
      <Children />
      <Sheet
        open
        onClose={noop}
        title="Give Ada time"
        description="Free, from you. It doesn’t cost Ada any points."
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="primary">Give 30 min of iPad time</Button>
          </>
        }
      >
        <div className="grid gap-6">
          <SegmentedControl
            label="Time for"
            value="personal"
            onChange={noop}
            options={[
              { value: 'personal', label: 'iPad' },
              { value: 'tv', label: 'TV' },
            ]}
          />
          <ChoiceGroup
            label="How long"
            onChoose={noop}
            choices={[
              { value: '15', label: '15 min' },
              { value: '30', label: '30 min' },
              { value: '60', label: '1 hour' },
            ]}
          />
          <div className="flex items-center gap-3">
            <CheckCircle checked label="Start it now on Ada’s iPad" />
            <span>Start it now on Ada’s iPad (even outside Ada’s hours)</span>
          </div>
        </div>
      </Sheet>
    </>
  );
}

/** A reward that is screen time: points buy minutes of iPad or TV time, with no adult step. */
export function RewardScreenTime() {
  return (
    <ManageShell nav={settingsNav('/manage/rewards')}>
      <PageHeader title="Edit reward" />
      <form className="grid max-w-xl gap-6" onSubmit={(e) => { e.preventDefault(); }}>
        <Input label="Name" defaultValue="30 min of iPad" maxLength={80} />
        <Input label="Cost in points" type="number" inputMode="numeric" min={1} defaultValue={20} />
        <SegmentedControl
          label="What it gives"
          value="personal"
          onChange={noop}
          options={[
            { value: 'thing', label: 'Something else' },
            { value: 'personal', label: 'iPad time' },
            { value: 'tv', label: 'TV time' },
          ]}
        />
        <Input
          label="Minutes"
          type="number"
          inputMode="numeric"
          min={1}
          max={240}
          defaultValue={30}
          hint="1 to 240. It’s saved on Ada’s iPad as soon as Ada chooses it; no need to approve."
        />
        <div className="flex gap-3">
          <Button variant="primary" type="submit">
            Save
          </Button>
          <ButtonLink href="#rewards">Cancel</ButtonLink>
        </div>
      </form>
    </ManageShell>
  );
}

/** Manage → Devices settings for every child's iPad: what the lock screen says, and blocked websites. */
export function DeviceSettings() {
  return (
    <ManageShell nav={settingsNav('/manage/devices')}>
      <PageHeader title="iPad settings" description="For every child’s iPad. They pick it up at their next sync, and keep it offline." />
      <Section title="Lock screen message" description="Under it, the iPad adds when screen time opens and how much time is saved.">
        <form className="grid max-w-xl gap-3" onSubmit={(e) => { e.preventDefault(); }}>
          <Textarea label="Message" rows={3} maxLength={LOCK_MESSAGE_MAX} defaultValue={DEFAULT_LOCK_MESSAGE} hint={`Up to ${String(LOCK_MESSAGE_MAX)} characters.`} />
          <div className="flex gap-3">
            <Button variant="primary" type="submit">
              Save message
            </Button>
            <Button variant="quiet">Use the default</Button>
          </div>
        </form>
      </Section>
      <Section title="Blocked websites" description="Browsers stay open; only these websites are blocked, in every browser on the iPad.">
        <form className="grid max-w-xl gap-4" onSubmit={(e) => { e.preventDefault(); }}>
          <Textarea
            label="Always blocked"
            rows={4}
            defaultValue={webFilter.gamePortals.join('\n')}
            hint="Game portals. One website per line, like games.example.test."
          />
          <Textarea
            label="Blocked unless iPad time is running"
            rows={5}
            defaultValue={webFilter.games.join('\n')}
            hint="Single games. One website per line."
          />
          <Notice tone="attention">An iPad applies up to 50 websites (always-blocked first). Parent setup on the iPad says if some didn’t fit.</Notice>
          <div>
            <Button variant="primary" type="submit">
              Save websites
            </Button>
          </div>
        </form>
      </Section>
    </ManageShell>
  );
}

/** A TV's state in a few words, for adults: what the router lets through and who is watching. */
function tvStatus(tv: Tv): { label: string; tone: 'neutral' | 'attention' | 'positive' | 'info' } {
  if (tv.openUntil !== null) return { label: `Online · open until ${tv.openUntil}`, tone: 'info' };
  if (tv.session?.watcher === 'parents') return { label: 'On · Parents', tone: 'positive' };
  if (tv.session) return { label: `On · ${tv.session.childName ?? 'A child'}`, tone: 'positive' };
  if (tv.prompt) return { label: 'Asking who’s watching', tone: 'attention' };
  return { label: 'Offline', tone: 'neutral' };
}

function tvMeta(tv: Tv): string {
  return [
    tv.room,
    tv.openHours ? `Open ${tv.openHours.start}–${tv.openHours.end}` : null,
    tv.haEntityId ? 'Home Assistant' : 'Router only',
  ]
    .filter(Boolean)
    .join(' · ');
}

/** Slice 9: shared TVs stay offline until someone is watching; adults see each TV and act on it. */
export function Tvs() {
  return (
    <ManageShell nav={settingsNav('/manage/devices')}>
      <PageHeader
        title="TVs"
        description="TVs stay offline until someone is watching. A child’s TV time runs only while they watch."
        actions={
          <>
            <Button>Open TVs…</Button>
            <Button variant="primary">Add a TV</Button>
          </>
        }
      />
      <Section title="TVs" count={tvs.length}>
        <List>
          {tvs.map((tv) => {
            const status = tvStatus(tv);
            return (
              <ListRow
                key={tv.id}
                stackOnNarrow
                title={tv.name}
                meta={
                  <>
                    <Tag tone={status.tone}>{status.label}</Tag> {tvMeta(tv)}
                  </>
                }
                trailing={
                  <>
                    {tv.session ? (
                      <Button aria-label={`Turn off ${tv.name}`}>Turn off</Button>
                    ) : (
                      <Button aria-label={`Turn on ${tv.name} for someone`}>Turn on for…</Button>
                    )}
                    <Button variant="quiet" aria-label={`Edit ${tv.name}`}>
                      Edit
                    </Button>
                  </>
                }
              />
            );
          })}
        </List>
      </Section>
    </ManageShell>
  );
}

/**
 * "Who's watching?": a TV turned on. In the inbox and as a push. Children who can't watch say why;
 * "Not now" keeps the TV offline until it is turned on again. One answer wins.
 */
export function TvWhoIsWatching() {
  const tv = tvs[0];
  if (!tv?.prompt) return null;
  return (
    <ManageShell
      nav={<ManageNav groups={manageGroups('/manage/inbox')} shortcut="/manage/inbox" back={{ href: '/today', label: 'Back to family' }} />}
    >
      <PageHeader title="Inbox" />
      <Section title="Waiting for you" count={1}>
        <List>
          <ListRow
            stackOnNarrow
            title={`${tv.name} is on. Who’s watching?`}
            meta={`Turned on at ${tv.prompt.raisedAt}`}
            trailing={
              <ChoiceGroup
                label={`Who’s watching ${tv.name}`}
                onChoose={noop}
                choices={[
                  ...tv.prompt.children.map((c) => ({
                    value: c.childId,
                    label: c.name,
                    detail: c.canWatch ? `${String(Math.floor(c.tvSecondsRemaining / 60))} min` : (c.reason ?? 'can’t watch now'),
                    disabled: !c.canWatch,
                  })),
                  { value: 'parents', label: 'Parents' },
                  { value: 'not_now', label: 'Not now', variant: 'quiet' as const },
                ]}
              />
            }
          />
        </List>
      </Section>
    </ManageShell>
  );
}

/** Adding or editing a TV: name, room, its network addresses, Home Assistant, and open hours. */
export function TvEdit() {
  const tv = tvs[3];
  return (
    <>
      <Tvs />
      <Sheet
        open
        onClose={noop}
        title={`Edit ${tv?.name ?? 'TV'}`}
        footer={
          <>
            <Button variant="danger">Remove TV…</Button>
            <Button>Cancel</Button>
            <Button variant="primary">Save TV</Button>
          </>
        }
      >
        <div className="grid gap-5">
          <Input label="Name" defaultValue={tv?.name} maxLength={80} hint="For example Den TV." required />
          <Input label="Room (optional)" defaultValue={tv?.room ?? ''} maxLength={80} />
          <Textarea
            label="MAC addresses"
            rows={3}
            defaultValue={tv?.macs.join('\n')}
            className="font-mono"
            hint="One per line, up to 8: the TV’s wired and Wi-Fi addresses, and any streaming box."
          />
          <Input
            label="Home Assistant entity (optional)"
            defaultValue={tv?.haEntityId ?? ''}
            className="font-mono"
            hint="For example media_player.den_tv. Home Assistant reports it turning on and off."
          />
          <Section title="Open hours" description="Online without asking, every day. An earlier end is the next morning.">
            <div className="grid grid-cols-2 gap-4">
              <Input type="time" label="Open from" defaultValue={tv?.openHours?.start} />
              <Input type="time" label="Until" defaultValue={tv?.openHours?.end} />
            </div>
          </Section>
        </div>
      </Sheet>
    </>
  );
}

/** Opening TVs: online for a while without asking; anyone watching is no longer charged. */
export function TvOpen() {
  return (
    <>
      <Tvs />
      <Sheet
        open
        onClose={noop}
        title="Open TVs"
        description="The router stops limiting them until then. Nobody’s TV time runs."
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="primary">Open 1 hour</Button>
          </>
        }
      >
        <div className="grid gap-5">
          <SegmentedControl
            label="Which TVs"
            value="all"
            onChange={noop}
            options={[
              { value: 'all', label: 'All TVs' },
              { value: 'some', label: 'Choose TVs' },
            ]}
          />
          <ChoiceGroup
            label="How long"
            onChoose={noop}
            choices={Object.entries(durationLabels).map(([value, label]) => ({ value, label }))}
          />
          <Notice tone="info">Ezra is watching Living room TV. Opening it ends Ezra’s session now; the rest of Ezra’s TV time stays.</Notice>
        </div>
      </Sheet>
    </>
  );
}

/**
 * Account (slices 10 and 11): how you sign in, and where you're signed in. Passkeys are named so you
 * can tell them apart; phones are listed so a lost one can be signed out from here.
 */
export function Account({ link = oidcLink }: { link?: OidcLink | null }) {
  return (
    <ManageShell nav={settingsNav('/manage/account')}>
      <PageHeader title="Account" description={`Signed in as ${me.email}.`} />
      <Section
        title="Passkeys"
        count={passkeys.length}
        description="Sign in with your face, fingerprint or a security key instead of your password."
        actions={<Button variant="primary">Add a passkey</Button>}
      >
        <List>
          {passkeys.map((pk) => (
            <ListRow
              key={pk.id}
              stackOnNarrow
              title={pk.name}
              meta={
                <>
                  <Tag tone={pk.backedUp ? 'positive' : 'neutral'}>{pk.backedUp ? 'Synced' : 'This device only'}</Tag>{' '}
                  {[`added ${pk.createdAt}`, pk.lastUsedAt === undefined ? 'not used yet' : `last used ${pk.lastUsedAt}`].join(' · ')}
                </>
              }
              trailing={
                <>
                  <Button variant="quiet" aria-label={`Rename ${pk.name}`}>
                    Rename
                  </Button>
                  <Button variant="quiet" aria-label={`Remove ${pk.name}`}>
                    Remove…
                  </Button>
                </>
              }
            />
          ))}
        </List>
      </Section>
      <Section title="Single sign-on" description="Your password and passkeys keep working either way.">
        {link ? (
          <List>
            <ListRow
              stackOnNarrow
              title={`Linked to ${link.providerName}`}
              meta={`${link.email} · since ${link.linkedAt}`}
              trailing={
                <Button variant="quiet" aria-label={`Unlink ${link.providerName}`}>
                  Unlink…
                </Button>
              }
            />
          </List>
        ) : (
          <p className="m-0 text-muted-foreground">
            Not linked. Choose “Sign in with Pocket ID” on the sign-in page once; it links when Pocket ID confirms {me.email}.
          </p>
        )}
      </Section>
      <Section title="Phones" count={phoneSessions.length} description="The OpenCozy app on your phone. Each stays signed in for 90 days.">
        <List>
          {phoneSessions.map((ps) => (
            <ListRow
              key={ps.id}
              stackOnNarrow
              title={ps.deviceName}
              meta={[
                `signed in ${ps.createdAt}`,
                `last used ${ps.lastUsedAt}`,
                ps.pushEnabled ? 'notifications on' : 'notifications off',
                `until ${ps.expiresAt}`,
              ].join(' · ')}
              trailing={
                <Button variant="quiet" aria-label={`Sign out ${ps.deviceName}`}>
                  Sign out…
                </Button>
              }
            />
          ))}
        </List>
      </Section>
    </ManageShell>
  );
}

export function AccountNotLinked() {
  return <Account link={null} />;
}

/** Naming a new passkey; then the browser asks for a face, fingerprint or key. */
export function PasskeyAdd() {
  return (
    <>
      <Account />
      <Sheet
        open
        onClose={noop}
        title="Add a passkey"
        description="Next, your browser asks you to use your face, fingerprint or a security key."
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="primary">Continue</Button>
          </>
        }
      >
        <Input label="Name" defaultValue="Laptop" maxLength={80} hint="So you can tell your passkeys apart, such as Laptop." required />
      </Sheet>
    </>
  );
}

export function PasskeyRename() {
  return (
    <>
      <Account />
      <Sheet
        open
        onClose={noop}
        title="Rename Security key"
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="primary">Save</Button>
          </>
        }
      >
        <Input label="Name" defaultValue="Security key" maxLength={80} required />
      </Sheet>
    </>
  );
}

export function PasskeyRemove() {
  return (
    <>
      <Account />
      <ConfirmDialog
        open
        onCancel={noop}
        onConfirm={noop}
        title="Remove Security key?"
        description="It won’t sign you in here any more. Also delete it from the key or password manager that holds it."
        confirmLabel="Remove Security key"
        tone="danger"
      />
    </>
  );
}

export function SsoUnlink() {
  return (
    <>
      <Account />
      <ConfirmDialog
        open
        onCancel={noop}
        onConfirm={noop}
        title="Unlink Pocket ID?"
        description="“Sign in with Pocket ID” stops working for you until you use it again. Your password and passkeys keep working."
        confirmLabel="Unlink Pocket ID"
        tone="danger"
      />
    </>
  );
}

export function PhoneSignOut() {
  return (
    <>
      <Account />
      <ConfirmDialog
        open
        onCancel={noop}
        onConfirm={noop}
        title="Sign out Old iPhone?"
        description="The app on it is signed out at once and stops getting notifications. Sign in on that phone to use it again."
        confirmLabel="Sign out Old iPhone"
        tone="danger"
      />
    </>
  );
}

/**
 * Editing a chore, including giving it to another child. A change applies from today: points already
 * earned stay where they are. A stale version shows the newer one instead of overwriting it.
 */
export function ChoreEdit({ conflict = false }: { conflict?: boolean }) {
  return (
    <>
      <Chores />
      <Sheet
        open
        onClose={noop}
        title="Edit Tidy the hallway"
        description="Changes apply from today. Points already earned stay."
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="primary">Save</Button>
          </>
        }
      >
        <div className="grid gap-5">
          {conflict && <Notice tone="attention">{catalogErrorText.version_conflict}</Notice>}
          <Input label="Chore" defaultValue="Tidy the hallway" maxLength={80} required />
          <SegmentedControl label="Whose chore" value="c3" onChange={noop} options={catalogKids.map((k) => ({ value: k.id, label: k.name }))} />
          <p className="m-0 text-muted-foreground">Moving it to Noor: it leaves Ada’s list today and shows on Noor’s.</p>
          <Input label="Points" type="number" inputMode="numeric" min={0} max={1000} defaultValue={5} />
          <Section title="Days">
            <DayPicker label="Days for Tidy the hallway" value={[1, 3, 5]} onChange={noop} />
          </Section>
        </div>
      </Sheet>
    </>
  );
}

export function ChoreEditConflict() {
  return <ChoreEdit conflict />;
}

/** Routines: a list of steps that earns its points when every step is checked. */
export function Routines() {
  return (
    <ManageShell
      nav={<ManageNav groups={manageGroups('/manage/routines')} shortcut="/manage/inbox" back={{ href: '/today', label: 'Back to family' }} />}
    >
      <PageHeader title="Routines" description="Steps a child checks off, worth points when all are done." actions={<Button variant="primary">Add routine</Button>} />
      <List>
        {routines.map((r) => {
          const kid = catalogKids.find((k) => k.id === r.childId);
          return (
            <ListRow
              key={r.id}
              stackOnNarrow
              leading={kid ? <ProfileAvatar name={kid.name} color={kid.color} size="small" aria-hidden="true" /> : undefined}
              title={`${kid?.name ?? 'A child'}’s ${r.title.toLowerCase()} routine`}
              meta={`${String(r.steps.length)} steps · ${pointsLabel(r.points)} · ${daysLabel(r.days)}`}
              trailing={
                <>
                  <Button variant="quiet" aria-label={`Edit ${r.title} for ${kid?.name ?? 'a child'}`}>
                    Edit
                  </Button>
                  <Button variant="quiet" aria-label={`Archive ${r.title} for ${kid?.name ?? 'a child'}`}>
                    Archive…
                  </Button>
                </>
              }
            />
          );
        })}
      </List>
    </ManageShell>
  );
}

/** Editing a routine: its steps in order (up to 20), moved with buttons, not dragging. */
export function RoutineEdit() {
  const r = routines[0];
  if (!r) return null;
  return (
    <>
      <Routines />
      <Sheet
        open
        onClose={noop}
        title="Edit Ada’s morning routine"
        description="Changes apply from today. Steps already checked today stay checked."
        footer={
          <>
            <Button>Cancel</Button>
            <Button variant="primary">Save</Button>
          </>
        }
      >
        <div className="grid gap-5">
          <Input label="Routine" defaultValue={r.title} maxLength={80} required />
          <SegmentedControl label="Whose routine" value={r.childId} onChange={noop} options={catalogKids.map((k) => ({ value: k.id, label: k.name }))} />
          <Input label="Points when every step is done" type="number" inputMode="numeric" min={0} max={1000} defaultValue={r.points} />
          <Section title="Days">
            <DayPicker label="Days for the morning routine" value={r.days} onChange={noop} />
          </Section>
          <Section title="Steps" count={r.steps.length} actions={<Button>Add a step</Button>}>
            <List>
              {r.steps.map((step, i) => (
                <ListRow
                  key={step.id}
                  title={<Input label={`Step ${String(i + 1)}`} defaultValue={step.title} maxLength={80} />}
                  trailing={
                    <>
                      <Button variant="quiet" disabled={i === 0} aria-label={`Move ${step.title} up`}>
                        Up
                      </Button>
                      <Button variant="quiet" disabled={i === r.steps.length - 1} aria-label={`Move ${step.title} down`}>
                        Down
                      </Button>
                      <Button variant="quiet" disabled={r.steps.length === 1} aria-label={`Remove ${step.title}`}>
                        Remove
                      </Button>
                    </>
                  }
                />
              ))}
            </List>
          </Section>
        </div>
      </Sheet>
    </>
  );
}

/** Rewards: what they cost, and whether children can choose them now. Edit uses RewardScreenTime's form. */
export function Rewards() {
  return (
    <ManageShell
      nav={<ManageNav groups={manageGroups('/manage/rewards')} shortcut="/manage/inbox" back={{ href: '/today', label: 'Back to family' }} />}
    >
      <PageHeader
        title="Rewards"
        description="What points buy."
        actions={
          <>
            <ButtonLink href="#redemptions">Chosen rewards</ButtonLink>
            <Button variant="primary">Add reward</Button>
          </>
        }
      />
      <List>
        {catalogRewards.map((r) => (
          <ListRow
            key={r.id}
            stackOnNarrow
            muted={!r.available}
            title={r.title}
            meta={
              <>
                {r.kind === 'screen_time' && <Tag tone="info">{`${String(r.minutes ?? 0)} min of iPad`}</Tag>}{' '}
                {[pointsLabel(r.cost), r.available ? null : 'hidden from children'].filter(Boolean).join(' · ')}
              </>
            }
            trailing={
              <>
                <Button variant="quiet" aria-label={`Edit ${r.title}`}>
                  Edit
                </Button>
                <Button variant="quiet" aria-label={`Archive ${r.title}`}>
                  Archive…
                </Button>
              </>
            }
          />
        ))}
      </List>
    </ManageShell>
  );
}

function redemptionMeta(r: RedemptionRow): string {
  const used = r.minutes === undefined ? null : `${String(r.usedMinutes ?? 0)} of ${String(r.minutes)} min used`;
  const refunded = r.refundedPoints === undefined ? null : `${pointsLabel(r.refundedPoints)} back`;
  return [r.child.name, pointsLabel(r.cost), r.requestedAt, used, refunded].filter(Boolean).join(' · ');
}

/**
 * Chosen rewards: what to give, what was given, what was cancelled. Cancelling refunds the points,
 * or, for screen time, the share not used yet.
 */
export function Redemptions() {
  const toGive = redemptions.filter((r) => r.status === 'approved');
  const given = redemptions.filter((r) => r.status === 'fulfilled');
  const cancelled = redemptions.filter((r) => r.status === 'cancelled');
  const avatar = (r: RedemptionRow) => <ProfileAvatar name={r.child.name} color={r.child.color} size="small" aria-hidden="true" />;
  return (
    <ManageShell
      nav={<ManageNav groups={manageGroups('/manage/rewards')} shortcut="/manage/inbox" back={{ href: '/today', label: 'Back to family' }} />}
    >
      <PageHeader title="Chosen rewards" description="Approved rewards to give, and what’s been given." />
      <Section title="To give" count={toGive.length}>
        <List>
          {toGive.map((r) => (
            <ListRow
              key={r.id}
              stackOnNarrow
              leading={avatar(r)}
              title={r.title}
              meta={redemptionMeta(r)}
              trailing={
                <>
                  <Button variant="quiet" aria-label={`Cancel ${r.title} for ${r.child.name} and refund`}>
                    Cancel and refund…
                  </Button>
                  <Button variant="primary" aria-label={`Mark ${r.title} as given to ${r.child.name}`}>
                    Given
                  </Button>
                </>
              }
            />
          ))}
        </List>
      </Section>
      <Section title="Given" count={given.length}>
        <List>
          {given.map((r) => {
            const back = refundFor(r);
            const canCancel = r.minutes !== undefined && (r.usedMinutes ?? 0) < r.minutes;
            return (
              <ListRow
                key={r.id}
                stackOnNarrow
                leading={avatar(r)}
                title={r.title}
                meta={redemptionMeta(r)}
                trailing={
                  canCancel ? (
                    <Button variant="quiet" aria-label={`Cancel the rest of ${r.title} for ${r.child.name}, ${pointsLabel(back)} back`}>
                      Cancel the rest…
                    </Button>
                  ) : undefined
                }
              />
            );
          })}
        </List>
      </Section>
      <Section title="Cancelled" count={cancelled.length}>
        <List>
          {cancelled.map((r) => (
            <ListRow key={r.id} muted leading={avatar(r)} title={r.title} meta={redemptionMeta(r)} />
          ))}
        </List>
      </Section>
    </ManageShell>
  );
}

/** Cancelling screen time that's partly used: the unused share comes back, rounded down. */
export function RedemptionCancel() {
  const r = redemptions[1];
  if (!r) return null;
  const back = refundFor(r);
  return (
    <>
      <Redemptions />
      <ConfirmDialog
        open
        onCancel={noop}
        onConfirm={noop}
        title={`Cancel the rest of Noor’s ${r.title}?`}
        description={`Noor has used ${String(r.usedMinutes ?? 0)} of ${String(r.minutes ?? 0)} minutes. The ${String((r.minutes ?? 0) - (r.usedMinutes ?? 0))} minutes left are removed and Noor gets ${pointsLabel(back)} back. Time already running keeps going.`}
        confirmLabel={`Cancel and refund ${pointsLabel(back)}`}
        cancelLabel="Keep it"
        tone="danger"
      />
    </>
  );
}

/** Household settings: the name everyone sees, and the time zone the household day follows. */
export function HouseholdSettings() {
  return (
    <ManageShell nav={settingsNav('/manage/household')}>
      <PageHeader title="Household" />
      <form className="grid max-w-xl gap-6" onSubmit={(e) => { e.preventDefault(); }}>
        <Input label="Name" defaultValue={household.name} maxLength={80} hint="On the wall display and the children’s screens." required />
        <Input
          label="Time zone"
          list="time-zones"
          defaultValue={household.timeZone}
          hint="The household day starts at midnight here: chores reset, and allowed hours and TV open hours follow it."
          required
        />
        <datalist id="time-zones">
          {timeZones.map((z) => (
            <option key={z} value={z} />
          ))}
        </datalist>
        <div>
          <Button variant="primary" type="submit">
            Save
          </Button>
        </div>
      </form>
    </ManageShell>
  );
}
