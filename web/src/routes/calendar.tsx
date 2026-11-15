import { Link } from '@tanstack/react-router';
import { useQueryClient } from '@tanstack/react-query';
import { useState, type SyntheticEvent } from 'react';

import {
  Button,
  CheckCircle,
  ConfirmDialog,
  EmptyState,
  Field,
  Input,
  List,
  ListRow,
  Notice,
  PageHeader,
  ProfileAvatar,
  profileColors,
  Section,
  SegmentedControl,
  Sheet,
  Tag,
} from '@app/design';

import { $api, errorCode, errorMessage, idempotencyKey, useAttempts } from '../api/client';
import type { components } from '../api/schema.gen';

type CalendarEvent = components['schemas']['CalendarEvent'];
type CalendarSource = components['schemas']['CalendarSource'];
type ErrorCode = components['schemas']['CalendarErrorCode'];
type Color = CalendarSource['color'];

// Sync errors, one sentence each for adults.
export const calendarErrorText: Record<ErrorCode, string> = {
  timeout: 'The calendar took too long to answer. OpenCozy will try again soon.',
  not_found: 'The calendar address no longer exists. Check the link, or remove it.',
  access_denied: 'The calendar refused access. It may have been made private.',
  too_large: 'The calendar is too big to load. Try a calendar with fewer past events.',
  invalid_calendar: 'That address isn’t a calendar OpenCozy can read.',
  too_many_events: 'The calendar has more events than OpenCozy can show. Older ones were left out.',
  rate_limited: 'The provider asked OpenCozy to slow down. It will try again later.',
  token_expired: 'The sign-in has expired. Sign in again to keep it up to date.',
};

const providerLabels: Record<CalendarSource['provider'], string> = { ics: 'Calendar link', google: 'Google', icloud: 'iCloud' };
const colorNames: Record<Color, string> = { rose: 'Rose', sage: 'Sage', sky: 'Sky', butter: 'Butter', lilac: 'Lilac', peach: 'Peach', teal: 'Teal', sand: 'Sand' };

/** A household day (YYYY-MM-DD) as words, with no time zone shift. */
function dayLabel(date: string, opts: Intl.DateTimeFormatOptions = { weekday: 'long', month: 'short', day: 'numeric' }) {
  const [y, m, d] = date.split('-').map(Number);
  return new Date(Date.UTC(y ?? 2000, (m ?? 1) - 1, d ?? 1)).toLocaleDateString(undefined, { ...opts, timeZone: 'UTC' });
}

function addDays(date: string, days: number) {
  const [y, m, d] = date.split('-').map(Number);
  return new Date(Date.UTC(y ?? 2000, (m ?? 1) - 1, (d ?? 1) + days)).toISOString().slice(0, 10);
}

/** Today in the household's time zone. */
function todayIn(zone: string) {
  return new Intl.DateTimeFormat('en-CA', { timeZone: zone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());
}

/** "3:30 PM" from an RFC3339 time already in household time. */
function clock(iso: string): string {
  const [h = 0, m = 0] = iso.slice(11, 16).split(':').map(Number);
  const hour = h % 12 === 0 ? 12 : h % 12;
  return `${String(hour)}:${String(m).padStart(2, '0')} ${h < 12 ? 'AM' : 'PM'}`;
}

/** When an event happens, as one short line. All-day ends are exclusive. */
function when(e: Pick<CalendarEvent, 'allDay' | 'start' | 'end'>): string {
  if (!e.allDay) return `${clock(e.start)} – ${clock(e.end)}`;
  const last = addDays(e.end, -1);
  return last === e.start ? 'All day' : `All day · until ${dayLabel(last, { weekday: 'short' })}`;
}

const titleOf = (e: Pick<CalendarEvent, 'title'>) => e.title ?? 'Busy';

function useChildren() {
  const children = $api.useQuery('get', '/children');
  const all = children.data?.items ?? [];
  return { all, live: all.filter((c) => !c.archived), name: (id: string) => all.find((c) => c.id === id)?.name ?? 'A child' };
}

// Manage: the week's events from the followed calendars, by day.
export function CalendarPage() {
  const household = $api.useQuery('get', '/household');
  const [start, setStart] = useState<string | undefined>(undefined);
  const zone = household.data?.timeZone;
  const from = start ?? (zone ? todayIn(zone) : undefined);
  const to = from ? addDays(from, 7) : undefined;
  const events = $api.useQuery('get', '/calendar', { params: { query: { from: from ?? '', to: to ?? '' } } }, { enabled: from !== undefined });
  const kids = useChildren();

  const days = from ? Array.from({ length: 7 }, (_, i) => addDays(from, i)) : [];
  // An event shows on its first day in the week (one that began earlier, on the first day).
  const byDay = new Map<string, CalendarEvent[]>();
  for (const e of events.data?.events ?? []) {
    const d = from && e.start.slice(0, 10) < from ? from : e.start.slice(0, 10);
    byDay.set(d, [...(byDay.get(d) ?? []), e]);
  }
  const today = zone ? todayIn(zone) : undefined;

  return (
    <section className="flex flex-col gap-6">
      <PageHeader
        title="Calendar"
        description={from ? `${dayLabel(from)} to ${dayLabel(addDays(from, 6))}.` : undefined}
        actions={
          from && (
            <>
              <Button onClick={() => { setStart(addDays(from, -7)); }}>Previous week</Button>
              <Button onClick={() => { setStart(addDays(from, 7)); }}>Next week</Button>
              <Link to="/calendars" className="oc-button oc-button--quiet">Calendars</Link>
            </>
          )
        }
      />
      {events.error && <Notice tone="error">{errorMessage(events.error)}</Notice>}
      {events.isSuccess && events.data.events.length === 0 && (
        <EmptyState title="Nothing this week" description="Events from the calendars you follow show here." variant="surface" />
      )}
      {days.map((d) => {
        const list = byDay.get(d);
        if (!list?.length) return null;
        return (
          <Section key={d} title={d === today ? 'Today' : d === (today && addDays(today, 1)) ? 'Tomorrow' : dayLabel(d)}>
            <List aria-label={dayLabel(d)}>
              {list.map((e) => (
                <ListRow
                  key={e.id}
                  stackOnNarrow
                  leading={<ProfileAvatar name={e.calendar.name} color={e.calendar.color} size="small" aria-hidden="true" />}
                  title={<Link to="/calendar/events/$eventId" params={{ eventId: e.id }}>{titleOf(e)}</Link>}
                  meta={[when(e), e.calendar.name, e.location, e.children.length ? `for ${e.children.map(kids.name).join(', ')}` : null].filter(Boolean).join(' · ')}
                  trailing={e.status === 'tentative' ? <Tag>Maybe</Tag> : undefined}
                />
              ))}
            </List>
          </Section>
        );
      })}
    </section>
  );
}

// Manage: one event, with its description, and who it's for.
export function CalendarEventPage({ eventId }: { eventId: string }) {
  const queryClient = useQueryClient();
  const event = $api.useQuery('get', '/calendar/events/{eventId}', { params: { path: { eventId } } });
  const kids = useChildren();
  const assign = $api.useMutation('put', '/calendar/events/{eventId}/children', {
    onSuccess: () => Promise.all([
      queryClient.invalidateQueries({ queryKey: ['get', '/calendar/events/{eventId}'] }),
      queryClient.invalidateQueries({ queryKey: ['get', '/calendar'] }),
    ]),
  });
  const attempts = useAttempts();
  const [picked, setPicked] = useState<string[] | null>(null);
  const [asking, setAsking] = useState(false);
  const [scope, setScope] = useState<'series' | 'occurrence'>('series');

  if (event.error) return <Notice tone="error">{errorMessage(event.error)}</Notice>;
  const e = event.data;
  if (!e) return null;
  const chosen = picked ?? e.children;
  const title = titleOf(e);

  const save = (s: 'series' | 'occurrence') => {
    const body = { children: chosen, scope: s };
    const attempt = JSON.stringify([eventId, body]);
    assign.mutate(
      { params: { ...attempts.params(attempt), path: { eventId } }, body },
      { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setPicked(null); setAsking(false); } },
    );
  };

  return (
    <section className="flex flex-col gap-6">
      <PageHeader
        title={title}
        description={`${dayLabel(e.start.slice(0, 10))} · ${when(e)} · ${e.calendar.name}${e.recurring ? ' · repeats' : ''}`}
        actions={<Link to="/calendar" className="oc-button oc-button--quiet">Back to calendar</Link>}
      />
      {e.location ? <p className="m-0">{e.location}</p> : null}
      {e.description ? <p className="m-0 whitespace-pre-line">{e.description}</p> : null}
      <Section title="For" description="Shown on their cards and the wall display.">
        <List aria-label="Children">
          {kids.live.map((c) => (
            <ListRow
              key={c.id}
              leading={<ProfileAvatar name={c.name} color={c.color} size="small" aria-hidden="true" />}
              title={c.name}
              trailing={
                <CheckCircle
                  checked={chosen.includes(c.id)}
                  label={`${title} is for ${c.name}`}
                  disabled={picked === null}
                  onClick={() => { setPicked(chosen.includes(c.id) ? chosen.filter((x) => x !== c.id) : [...chosen, c.id]); }}
                />
              }
            />
          ))}
        </List>
        {assign.error && !asking && <Notice tone="error">{errorMessage(assign.error)}</Notice>}
        <div className="mt-4 flex gap-3">
          {picked === null ? (
            <Button onClick={() => { setPicked(e.children); assign.reset(); }}>Change who it’s for</Button>
          ) : (
            <>
              <Button onClick={() => { setPicked(null); }}>Cancel</Button>
              <Button variant="primary" disabled={assign.isPending} onClick={() => { if (e.recurring) setAsking(true); else save('series'); }}>
                Save
              </Button>
            </>
          )}
        </div>
      </Section>
      <Sheet
        open={asking}
        onClose={() => { setAsking(false); }}
        title={`Save who ${title} is for?`}
        description={`${title} repeats.`}
        footer={
          <>
            <Button onClick={() => { setAsking(false); }}>Cancel</Button>
            <Button variant="primary" disabled={assign.isPending} onClick={() => { save(scope); }}>Save</Button>
          </>
        }
      >
        <div className="grid gap-4">
          <SegmentedControl
            label="Apply to"
            value={scope}
            onChange={setScope}
            options={[
              { value: 'occurrence', label: 'Just this one' },
              { value: 'series', label: 'Every time' },
            ]}
          />
          {assign.error && <Notice tone="error">{errorMessage(assign.error)}</Notice>}
        </div>
      </Sheet>
    </section>
  );
}

interface SourceForm {
  id?: string;
  version?: number;
  name: string;
  url: string;
  color: Color;
  showOnDisplay: boolean;
}

const syncedLabel = (s: CalendarSource) =>
  s.lastSyncAt === undefined ? 'Not synced yet' : `Synced ${new Date(s.lastSyncAt).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })}`;

// Manage: the followed calendars, their sync state, and adding a calendar link. A link's address
// is write-only: it is never shown again.
export function CalendarsPage() {
  const queryClient = useQueryClient();
  const sources = $api.useQuery('get', '/calendars');
  const refresh = () => Promise.all([
    queryClient.invalidateQueries({ queryKey: ['get', '/calendars'] }),
    queryClient.invalidateQueries({ queryKey: ['get', '/calendar'] }),
  ]);
  const add = $api.useMutation('post', '/calendars', { onSuccess: refresh });
  const update = $api.useMutation('patch', '/calendars/{calendarId}', { onSuccess: refresh });
  const remove = $api.useMutation('delete', '/calendars/{calendarId}', { onSuccess: refresh });
  const attempts = useAttempts();
  const [form, setForm] = useState<SourceForm | null>(null);
  const [removing, setRemoving] = useState<CalendarSource | null>(null);

  const list = sources.data?.calendars ?? [];
  const trouble = list.filter((s) => s.status !== 'ok');
  const open = (f: SourceForm) => {
    setForm(f);
    add.reset();
    update.reset();
  };
  const close = () => { setForm(null); };

  const submit = (ev: SyntheticEvent) => {
    ev.preventDefault();
    if (!form) return;
    const fields = { name: form.name.trim(), color: form.color, showOnDisplay: form.showOnDisplay };
    if (form.id === undefined || form.version === undefined) {
      const body = { ...fields, url: form.url.trim() };
      const attempt = JSON.stringify(body);
      add.mutate({ params: attempts.params(attempt), body }, { onSettled: attempts.settleIfAnswered(attempt), onSuccess: close });
      return;
    }
    const body = { ...fields, expectedVersion: form.version };
    const attempt = JSON.stringify([form.id, body]);
    update.mutate(
      { params: { ...attempts.params(attempt), path: { calendarId: form.id } }, body },
      {
        onSettled: attempts.settleIfAnswered(attempt),
        onSuccess: close,
        onError: (error) => { if (errorCode(error) === 'version_conflict') void refresh(); },
      },
    );
  };

  const saving = add.isPending || update.isPending;
  const saveError = add.error ?? update.error;

  return (
    <section className="flex flex-col gap-6">
      <PageHeader
        title="Calendars"
        description="Choose which ones show on the wall display."
        actions={
          <>
            <Button variant="primary" onClick={() => { open({ name: '', url: '', color: 'sky', showOnDisplay: true }); }}>Add a calendar link</Button>
            <Link to="/calendar" className="oc-button oc-button--quiet">Calendar</Link>
          </>
        }
      />
      {trouble.length > 0 && (
        <Notice tone="attention" title={trouble.length === 1 ? '1 calendar needs attention' : `${String(trouble.length)} calendars need attention`}>
          {trouble.map((s) => s.name).join(', ')}: see below.
        </Notice>
      )}
      {list.length ? (
        <Section title="Connected" count={list.length}>
          <List aria-label="Calendars">
            {list.map((s) => (
              <ListRow
                key={s.id}
                stackOnNarrow
                leading={<ProfileAvatar name={s.name} color={s.color} size="small" aria-hidden="true" />}
                title={s.name}
                meta={
                  <>
                    {[providerLabels[s.provider], s.showOnDisplay ? 'On the wall display' : 'Adults only', syncedLabel(s)].join(' · ')}
                    {s.error ? <span className="mt-1 block">{calendarErrorText[s.error]}</span> : null}
                  </>
                }
                trailing={
                  <>
                    {s.status === 'error' ? <Tag tone="error">Not updating</Tag> : null}
                    <Button variant="quiet" aria-label={`Edit ${s.name}`} onClick={() => { open({ id: s.id, version: s.version, name: s.name, url: '', color: s.color, showOnDisplay: s.showOnDisplay }); }}>
                      Edit
                    </Button>
                  </>
                }
              />
            ))}
          </List>
        </Section>
      ) : (
        sources.isSuccess && <EmptyState title="No calendars yet" description="Add a calendar link from school, a club or your own calendar app." variant="surface" />
      )}

      <Sheet
        open={form !== null}
        onClose={close}
        title={form?.id ? `Edit ${form.name}` : 'Add a calendar link'}
        description={form?.id ? undefined : 'Paste the private address from the calendar’s sharing settings.'}
        footer={
          <>
            {form?.id && (
              <Button variant="danger" onClick={() => { const s = list.find((x) => x.id === form.id); if (s) { close(); setRemoving(s); } }}>
                Remove…
              </Button>
            )}
            <Button onClick={close}>Cancel</Button>
            <Button variant="primary" type="submit" form="calendar-source" disabled={saving}>
              {form?.id ? 'Save' : 'Add calendar'}
            </Button>
          </>
        }
      >
        {form && (
          <form id="calendar-source" onSubmit={submit} className="grid gap-4" aria-label={form.id ? 'Edit calendar' : 'Add a calendar link'}>
            <Input label="Name" maxLength={80} value={form.name} onChange={(e) => { setForm({ ...form, name: e.target.value }); }} required />
            {form.id === undefined && (
              <Input
                label="Calendar address"
                type="url"
                inputMode="url"
                maxLength={2048}
                hint="Starts with https:// or webcal://. OpenCozy never shows it again."
                value={form.url}
                onChange={(e) => { setForm({ ...form, url: e.target.value }); }}
                required
                autoComplete="off"
              />
            )}
            <Field label="Colour" htmlFor="calendar-color">
              <select id="calendar-color" value={form.color} onChange={(e) => { const c = profileColors.find((x) => x === e.target.value); if (c) setForm({ ...form, color: c }); }}>
                {profileColors.map((c) => (
                  <option key={c} value={c}>{colorNames[c]}</option>
                ))}
              </select>
            </Field>
            <SegmentedControl
              label="Who sees it"
              value={form.showOnDisplay ? 'display' : 'adults'}
              onChange={(v) => { setForm({ ...form, showOnDisplay: v === 'display' }); }}
              options={[
                { value: 'display', label: 'On the wall display' },
                { value: 'adults', label: 'Adults only' },
              ]}
            />
            {saveError && <Notice tone="error">{errorMessage(saveError)}</Notice>}
          </form>
        )}
      </Sheet>

      <ConfirmDialog
        open={removing !== null}
        title={removing ? `Remove ${removing.name}?` : ''}
        description="Its events leave OpenCozy, along with who they were for. To follow it again, add the link again."
        confirmLabel="Remove"
        tone="danger"
        pending={remove.isPending}
        error={remove.error ? errorMessage(remove.error) : undefined}
        onCancel={() => { setRemoving(null); remove.reset(); }}
        onConfirm={() => {
          if (!removing) return;
          remove.mutate(
            { params: { ...idempotencyKey(), path: { calendarId: removing.id }, query: { expectedVersion: removing.version } } },
            { onSuccess: () => { setRemoving(null); } },
          );
        }}
      />
    </section>
  );
}
