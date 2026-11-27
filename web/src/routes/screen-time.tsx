import { useQueryClient } from '@tanstack/react-query';
import { useState, type SyntheticEvent } from 'react';
import { Users } from 'lucide-react';

import {
  Button,
  ButtonPrimary,
  Checkbox,
  ConfirmDialog,
  Countdown,
  EmptyState,
  Input,
  List,
  ListRow,
  Notice,
  PageHeader,
  Panel,
  ProfileAvatar,
  Section,
  SegmentedControl,
  Sheet,
  Tag,
  Textarea,
} from '@app/design';

import { $api, errorMessage, useAttempts, type Child } from '../api/client';
import { RouterButtonLink } from '../link';
import type { components } from '../api/schema.gen';
import { EditError } from './edit-error';
import {
  creditLeft,
  creditTitle,
  domainsFromText,
  minutesLabel,
  screenState,
  useServerNow,
  type Credit,
  type ScreenTime,
} from './screen-time-logic';

type AllowedHours = components['schemas']['AllowedHours'];
type WebFilter = components['schemas']['WebFilter'];

const clockTime = new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' });
const day = new Intl.DateTimeFormat(undefined, { weekday: 'short', day: 'numeric', month: 'short' });
const giveMinutes = ['15', '30', '60', '120'] as const;

// Manage: each child's iPad time (running, queued, banked) and when it may be used, then the
// household's blocked websites. Everything a parent does about screen time, on one page.
export function ScreenTimePage() {
  const children = $api.useQuery('get', '/children');
  const kids = children.data?.items ?? [];
  return (
    <>
      <PageHeader title="Screen time" description="iPad time each child can use, and when. Time bought with points is cancelled under Chosen rewards." />
      {children.isError && <Notice tone="error">{errorMessage(children.error)}</Notice>}
      {kids.map((c) => (
        <ChildScreenTime key={c.id} child={c} />
      ))}
      {children.isSuccess && kids.length === 0 && <EmptyState variant="surface" icon={Users} title="No children yet" description="Add a child to give them iPad time." action={<RouterButtonLink to="/children" variant="primary">Add a child</RouterButtonLink>} />}
      <BlockedWebsites />
    </>
  );
}

function ChildScreenTime({ child }: { child: Child }) {
  const queryClient = useQueryClient();
  const path = { params: { path: { childId: child.id } } };
  const query = $api.useQuery('get', '/children/{childId}/screen-time', path, { refetchInterval: 30_000 });
  const now = useServerNow(query.data?.serverTime, query.dataUpdatedAt);
  // Every change answers with the new screen time: show it at once, then let others refetch.
  const put = (st: ScreenTime) => {
    queryClient.setQueryData(['get', '/children/{childId}/screen-time', path], st);
    void queryClient.invalidateQueries({ queryKey: ['get', '/inbox'] });
  };
  const grant = $api.useMutation('post', '/children/{childId}/screen-time/grant', { onSuccess: (d) => { put(d.screenTime); } });
  const end = $api.useMutation('post', '/children/{childId}/screen-time/end', { onSuccess: (d) => { put(d.screenTime); } });
  const forfeit = $api.useMutation('post', '/children/{childId}/screen-time/credits/{creditId}/forfeit', { onSuccess: (d) => { put(d.screenTime); } });
  const attempts = useAttempts();

  const [giving, setGiving] = useState(false);
  const [ending, setEnding] = useState<'now' | 'queued' | null>(null);
  const [takingBack, setTakingBack] = useState<Credit | null>(null);
  const [editingHours, setEditingHours] = useState(false);

  const st = query.data;
  const s = st ? screenState(st, now) : undefined;

  const endTime = (scope: 'now' | 'queued', refund: boolean) => {
    const attempt = `end:${child.id}:${scope}:${String(refund)}:${String(st?.windows.length)}`;
    end.mutate(
      { params: { ...attempts.params(attempt), path: { childId: child.id } }, body: { scope, refund } },
      { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setEnding(null); } },
    );
  };
  const takeBack = (c: Credit) => {
    const attempt = `forfeit:${c.id}`;
    forfeit.mutate(
      { params: { ...attempts.params(attempt), path: { childId: child.id, creditId: c.id } }, body: {} },
      { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setTakingBack(null); } },
    );
  };

  const status = !s ? null : s.runningSeconds > 0 ? (
    <Tag tone="positive">
      On · <Countdown seconds={s.runningSeconds} />
    </Tag>
  ) : (
    <Tag tone="neutral">Locked</Tag>
  );
  const hours = st?.allowedHours;
  const opens = st?.hoursNow && !st.hoursNow.open && st.hoursNow.opensAt ? clockTime.format(new Date(st.hoursNow.opensAt)) : null;

  return (
    <Panel
      heading={
        <span className="flex items-center gap-3">
          <ProfileAvatar name={child.name} color={child.color} size="small" aria-hidden="true" />
          {child.name}
          {status}
        </span>
      }
      aria-label={`${child.name}’s screen time`}
    >
      {query.isError && <Notice tone="error">{errorMessage(query.error)}</Notice>}
      {st && s && (
        <div className="flex flex-col gap-5">
          <p className="m-0 text-muted-foreground">
            {[
              `${minutesLabel(s.bankedMinutes)} banked`,
              s.queuedSeconds > 0 ? `${minutesLabel(Math.ceil(s.queuedSeconds / 60))} queued` : null,
              hours ? `Allowed ${hours.schoolNight.start}–${hours.schoolNight.end} school nights, ${hours.weekend.start}–${hours.weekend.end} weekends` : 'Allowed any time',
              opens ? `opens at ${opens}` : null,
            ]
              .filter(Boolean)
              .join(' · ')}
          </p>
          {st.timeRequest?.state === 'pending' && <Notice tone="attention">{child.name} asked for more time. Answer it in the Inbox.</Notice>}
          <div className="flex flex-wrap gap-3">
            <ButtonPrimary onClick={() => { grant.reset(); setGiving(true); }}>Give time…</ButtonPrimary>
            {s.runningSeconds > 0 && (
              <Button variant="danger" onClick={() => { end.reset(); setEnding('now'); }}>End now…</Button>
            )}
            {s.queuedSeconds > 0 && (
              <Button onClick={() => { end.reset(); setEnding('queued'); }}>Cancel queued…</Button>
            )}
            <Button variant="quiet" onClick={() => { setEditingHours(true); }}>Allowed hours…</Button>
          </div>
          {st.credits.length > 0 && (
            <List aria-label={`${child.name}’s banked time`}>
              {st.credits.map((c) => (
                <ListRow
                  key={c.id}
                  title={creditTitle(c)}
                  meta={[`${minutesLabel(creditLeft(c))} left of ${minutesLabel(c.minutes)}`, day.format(new Date(c.createdAt))].join(' · ')}
                  trailing={
                    c.source === 'redemption' ? null : (
                      <Button variant="quiet" aria-label={`Take back: ${creditTitle(c)}, ${child.name}`} onClick={() => { forfeit.reset(); setTakingBack(c); }}>
                        Take back…
                      </Button>
                    )
                  }
                />
              ))}
            </List>
          )}
        </div>
      )}

      <GiveTime
        open={giving}
        child={child}
        pending={grant.isPending}
        error={grant.error}
        onClose={() => { setGiving(false); }}
        onGive={(minutes, startNow) => {
          const attempt = `grant:${child.id}:${String(minutes)}:${String(startNow)}`;
          grant.mutate(
            { params: { ...attempts.params(attempt), path: { childId: child.id } }, body: { minutes, startNow } },
            { onSettled: attempts.settleIfAnswered(attempt), onSuccess: () => { setGiving(false); } },
          );
        }}
      />
      <EndTime
        scope={ending}
        child={child}
        pending={end.isPending}
        error={end.error}
        onCancel={() => { setEnding(null); }}
        onEnd={endTime}
      />
      <ConfirmDialog
        open={takingBack !== null}
        title={takingBack ? `Take back ${minutesLabel(creditLeft(takingBack))} from ${child.name}?` : ''}
        description="The unused time leaves the bank. Time already started isn’t touched."
        confirmLabel="Take back"
        tone="danger"
        pending={forfeit.isPending}
        error={forfeit.error ? errorMessage(forfeit.error) : undefined}
        onConfirm={() => { if (takingBack) takeBack(takingBack); }}
        onCancel={() => { setTakingBack(null); }}
      />
      {editingHours && st && (
        <AllowedHoursSheet child={child} hours={st.allowedHours} onClose={() => { setEditingHours(false); }} onSaved={(next) => { put(next); setEditingHours(false); }} />
      )}
    </Panel>
  );
}

function GiveTime({
  open,
  child,
  pending,
  error,
  onClose,
  onGive,
}: {
  open: boolean;
  child: Child;
  pending: boolean;
  error: unknown;
  onClose: () => void;
  onGive: (minutes: number, startNow: boolean) => void;
}) {
  const [minutes, setMinutes] = useState<(typeof giveMinutes)[number]>('30');
  const [startNow, setStartNow] = useState(false);
  return (
    <Sheet
      open={open}
      onClose={onClose}
      title={`Give ${child.name} iPad time`}
      description="It goes into the bank for them to start, or starts now."
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <ButtonPrimary disabled={pending} onClick={() => { onGive(Number(minutes), startNow); }}>
            Give {minutesLabel(Number(minutes))}
          </ButtonPrimary>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {error ? <Notice tone="error">{errorMessage(error)}</Notice> : null}
        <SegmentedControl label="How long" value={minutes} onChange={setMinutes} options={giveMinutes.map((m) => ({ value: m, label: minutesLabel(Number(m)) }))} />
        <Checkbox label="Start now" hint="Unlocks the iPad straight away, within allowed hours." checked={startNow} onChange={(e) => { setStartNow(e.target.checked); }} />
      </div>
    </Sheet>
  );
}

function EndTime({
  scope,
  child,
  pending,
  error,
  onCancel,
  onEnd,
}: {
  scope: 'now' | 'queued' | null;
  child: Child;
  pending: boolean;
  error: unknown;
  onCancel: () => void;
  onEnd: (scope: 'now' | 'queued', refund: boolean) => void;
}) {
  const [refund, setRefund] = useState(true);
  return (
    <ConfirmDialog
      open={scope !== null}
      title={scope === 'queued' ? `Cancel ${child.name}’s queued time?` : `End ${child.name}’s iPad time now?`}
      description={scope === 'queued' ? 'What is running now carries on.' : 'The iPad locks within a few minutes.'}
      confirmLabel={scope === 'queued' ? 'Cancel queued time' : 'End now'}
      tone="danger"
      pending={pending}
      error={error ? errorMessage(error) : undefined}
      onConfirm={() => { if (scope) onEnd(scope, refund); }}
      onCancel={onCancel}
    >
      <Checkbox
        label="Put the unused time back in the bank"
        hint="Otherwise it’s gone. Time from a cancelled reward or taken back never returns."
        checked={refund}
        onChange={(e) => { setRefund(e.target.checked); }}
      />
    </ConfirmDialog>
  );
}

const defaultHours: AllowedHours = { schoolNight: { start: '15:30', end: '19:30' }, weekend: { start: '08:00', end: '20:00' } };

function AllowedHoursSheet({ child, hours, onClose, onSaved }: { child: Child; hours: AllowedHours | undefined; onClose: () => void; onSaved: (st: ScreenTime) => void }) {
  const save = $api.useMutation('put', '/children/{childId}/allowed-hours', { onSuccess: (d) => { onSaved(d); } });
  const attempts = useAttempts();
  const [limited, setLimited] = useState(hours !== undefined);
  const [h, setH] = useState<AllowedHours>(hours ?? defaultHours);
  const set = (part: keyof AllowedHours, edge: 'start' | 'end') => (e: { target: { value: string } }) => {
    setH((old) => ({ ...old, [part]: { ...old[part], [edge]: e.target.value } }));
  };
  const submit = (e: SyntheticEvent) => {
    e.preventDefault();
    const body = { allowedHours: limited ? h : null };
    const attempt = `hours:${child.id}:${JSON.stringify(body)}`;
    save.mutate({ params: { ...attempts.params(attempt), path: { childId: child.id } }, body }, { onSettled: attempts.settleIfAnswered(attempt) });
  };
  return (
    <Sheet
      open
      onClose={onClose}
      title={`When ${child.name} can use iPad time`}
      description="School nights are Sunday to Thursday. Time running at the end of the hours stops, and the rest stays banked."
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <ButtonPrimary type="submit" form="allowed-hours" disabled={save.isPending}>Save</ButtonPrimary>
        </>
      }
    >
      <form id="allowed-hours" onSubmit={submit} className="flex flex-col gap-4" aria-label={`${child.name}’s allowed hours`}>
        <EditError error={save.error} />
        <Checkbox label="Only at certain times" hint="Off: any time of day." checked={limited} onChange={(e) => { setLimited(e.target.checked); }} />
        {limited && (
          <>
            <div className="grid grid-cols-2 gap-4">
              <Input type="time" label="School nights from" value={h.schoolNight.start} onChange={set('schoolNight', 'start')} required />
              <Input type="time" label="until" value={h.schoolNight.end} onChange={set('schoolNight', 'end')} required />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <Input type="time" label="Weekends from" value={h.weekend.start} onChange={set('weekend', 'start')} required />
              <Input type="time" label="until" value={h.weekend.end} onChange={set('weekend', 'end')} required />
            </div>
          </>
        )}
      </form>
    </Sheet>
  );
}

// The household's blocked websites: game portals always, games unless iPad time is running.
function BlockedWebsites() {
  const queryClient = useQueryClient();
  const filter = $api.useQuery('get', '/household/web-filter');
  const save = $api.useMutation('put', '/household/web-filter', {
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['get', '/household/web-filter'] });
      await queryClient.invalidateQueries({ queryKey: ['get', '/children/{childId}/screen-time'] });
    },
  });
  return (
    <Section title="Blocked websites" description="On the children’s iPads and the home network. Each name covers its subdomains: example.com blocks www.example.com too.">
      {filter.isError && <Notice tone="error">{errorMessage(filter.error)}</Notice>}
      {filter.data && (
        <Panel>
          <WebFilterForm key={JSON.stringify(filter.data)} filter={filter.data} pending={save.isPending} error={save.error} saved={save.isSuccess} onSave={(body, params) => { save.mutate({ params, body }); }} />
        </Panel>
      )}
    </Section>
  );
}

function WebFilterForm({
  filter,
  pending,
  error,
  saved,
  onSave,
}: {
  filter: WebFilter;
  pending: boolean;
  error: unknown;
  saved: boolean;
  onSave: (body: WebFilter, params: { header: { 'Idempotency-Key': string } }) => void;
}) {
  const attempts = useAttempts();
  const [portals, setPortals] = useState(filter.gamePortals.join('\n'));
  const [games, setGames] = useState(filter.games.join('\n'));
  const submit = (e: SyntheticEvent) => {
    e.preventDefault();
    const body = { gamePortals: domainsFromText(portals), games: domainsFromText(games) };
    onSave(body, attempts.params(`web-filter:${JSON.stringify(body)}`));
  };
  return (
    <form onSubmit={submit} className="grid max-w-xl gap-5" aria-label="Blocked websites">
      {error ? <Notice tone="error">{errorMessage(error)}</Notice> : saved ? <Notice tone="success">Saved.</Notice> : null}
      <Textarea label="Always blocked" hint="Game portals and other sites that are never allowed. One per line." rows={5} value={portals} onChange={(e) => { setPortals(e.target.value); }} />
      <Textarea label="Blocked unless iPad time is running" hint="Games that count as screen time. One per line." rows={5} value={games} onChange={(e) => { setGames(e.target.value); }} />
      <div>
        <ButtonPrimary type="submit" disabled={pending}>Save</ButtonPrimary>
      </div>
    </form>
  );
}
