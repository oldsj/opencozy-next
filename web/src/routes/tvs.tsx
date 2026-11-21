import { useQueryClient } from '@tanstack/react-query';
import { useState, type SyntheticEvent } from 'react';

import {
  Button,
  Checkbox,
  CheckboxGroup,
  ChoiceGroup,
  ConfirmDialog,
  Countdown,
  DayPicker,
  EmptyState,
  Input,
  List,
  ListRow,
  Notice,
  PageHeader,
  Section,
  SegmentedControl,
  Sheet,
  Tag,
  Textarea,
} from '@app/design';

import { $api, errorMessage, useAttempts } from '../api/client';
import { EditError } from './edit-error';
import {
  clockLabel,
  clockSkew,
  endReasonLabel,
  fromWeekdays,
  minutesLabel,
  openHours,
  openHoursLabel,
  parseMacs,
  promptSource,
  secondsUntil,
  tvTitle,
  TV_REFRESH_MS,
  useTicking,
  watcherLabel,
  whoChoices,
  type OpenDuration,
  type TvChild,
  type TvList,
  type TvState,
  type WhoChoice,
} from './tv';

const tvsKey = { queryKey: ['get', '/tvs'] };

/** Every TV and the children's TV time, refreshed, with the server's clock. */
export function useTvs() {
  const tvs = $api.useQuery('get', '/tvs', {}, { refetchInterval: TV_REFRESH_MS });
  const skew = clockSkew(tvs.data?.serverTime, tvs.dataUpdatedAt || Date.now());
  return { tvs, skew };
}

/**
 * "Who's watching?" for one TV that is asking: Parents, a child (their time left, or why not),
 * Open 1 hour, Not now. One tap answers; the first answer anywhere wins.
 */
export function WhoIsWatching({ tv, kids }: { tv: TvState; kids: readonly TvChild[] }) {
  const queryClient = useQueryClient();
  // Answered here, on the TVs page or in the inbox: both show it.
  const refresh = () => Promise.all([queryClient.invalidateQueries(tvsKey), queryClient.invalidateQueries({ queryKey: ['get', '/inbox'] })]);
  // A tap retried after a lost answer reuses its key, so the server answers it once.
  const attempts = useAttempts();
  const answer = $api.useMutation('post', '/tv-prompts/{promptId}/answer', { onSettled: refresh });
  const open = $api.useMutation('post', '/tvs/open', { onSettled: refresh });
  const prompt = tv.prompt;
  if (!prompt) return null;
  const choose = (choice: WhoChoice) => {
    if (choice === 'open') {
      const attempt = `open:${tv.id}`;
      open.mutate({ params: attempts.params(attempt), body: { tvIds: [tv.id], duration: '1h' } }, { onSettled: attempts.settleIfAnswered(attempt) });
      return;
    }
    const child = choice.startsWith('child:') ? choice.slice('child:'.length) : undefined;
    const attempt = `answer:${prompt.id}:${choice}`;
    answer.mutate(
      {
        params: { ...attempts.params(attempt), path: { promptId: prompt.id } },
        body: { expectedVersion: prompt.version, answer: child ? 'child' : (choice as 'parents' | 'not_now'), ...(child ? { childId: child } : {}) },
      },
      { onSettled: attempts.settleIfAnswered(attempt) },
    );
  };
  const error = answer.error ?? open.error;
  return (
    <div className="grid gap-3">
      <ChoiceGroup label={`Who’s watching ${tv.name}?`} busy={answer.isPending || open.isPending} onChoose={choose} choices={whoChoices(tv.id, kids, { open: true, notNow: true })} />
      {error && <Notice tone="error">{errorMessage(error)}</Notice>}
    </div>
  );
}

/** The TVs asking "Who's watching?" (the inbox's `tvs`), for its top. Nothing when none are. */
export function TvsAsking({ list }: { list: TvList | undefined }) {
  const asking = list?.tvs.filter((t) => t.prompt) ?? [];
  if (!asking.length) return null;
  const kids = list?.children ?? [];
  return (
    <Section title="Who’s watching?" count={asking.length} description="These TVs are on, and stay offline until you choose.">
      <List aria-label="TVs asking who’s watching">
        {asking.map((tv) => (
          <ListRow key={tv.id} title={tvTitle(tv)} meta={tv.prompt ? `${promptSource(tv.prompt)} at ${clockLabel(tv.prompt.createdAt)}` : undefined}>
            <WhoIsWatching tv={tv} kids={kids} />
          </ListRow>
        ))}
      </List>
    </Section>
  );
}

interface TvForm {
  id?: string;
  version?: number;
  name: string;
  room: string;
  macs: string;
  entity: string;
  hours: boolean;
  start: string;
  end: string;
  days: number[];
}

const emptyTv: TvForm = { name: '', room: '', macs: '', entity: '', hours: false, start: '21:00', end: '07:00', days: [] };
const formOf = (t: TvState): TvForm => ({
  id: t.id,
  version: t.version,
  name: t.name,
  room: t.room ?? '',
  macs: t.macs.join('\n'),
  entity: t.haEntityId ?? '',
  hours: t.openHours !== null,
  start: t.openHours?.start ?? '21:00',
  end: t.openHours?.end ?? '07:00',
  days: fromWeekdays(t.openHours?.days),
});

const durations: { value: OpenDuration; label: string }[] = [
  { value: '1h', label: '1 hour' },
  { value: '2h', label: '2 hours' },
  { value: 'all_day', label: 'All day' },
];

// Manage: the household's TVs. Each row says what the TV is doing now and offers the one thing to
// do about it; set-up (MACs, Home Assistant, open hours) is behind Edit.
export function TvsPage() {
  const queryClient = useQueryClient();
  const { tvs, skew } = useTvs();
  const history = $api.useQuery('get', '/tvs/history', {}, { refetchInterval: 60_000 });
  const refresh = () => Promise.all([queryClient.invalidateQueries(tvsKey), queryClient.invalidateQueries({ queryKey: ['get', '/tvs/history'] })]);
  // Each action keeps its key until the server answers: a tap retried after a lost answer (+1 hr
  // above all) is the same command, not a second one.
  const attempts = useAttempts();
  const keyed = (attempt: string) => ({ params: attempts.params(attempt), settled: { onSettled: attempts.settleIfAnswered(attempt) } });
  const end = $api.useMutation('post', '/tvs/{tvId}/session/end', { onSettled: refresh });
  const extend = $api.useMutation('post', '/tvs/extend', { onSettled: refresh });
  const close = $api.useMutation('post', '/tvs/close', { onSettled: refresh });
  const open = $api.useMutation('post', '/tvs/open', { onSettled: refresh });
  const create = $api.useMutation('post', '/tvs', { onSuccess: refresh });
  const update = $api.useMutation('put', '/tvs/{tvId}', { onSuccess: refresh });
  const remove = $api.useMutation('delete', '/tvs/{tvId}', { onSettled: refresh });

  const [editing, setEditing] = useState<TvForm | null>(null);
  const [removing, setRemoving] = useState<TvState | null>(null);
  const [opening, setOpening] = useState(false);
  const [picked, setPicked] = useState<string[]>([]);
  const [duration, setDuration] = useState<OpenDuration>('1h');

  const list = tvs.data?.tvs ?? [];
  const kids = tvs.data?.children ?? [];
  const ends = list.map((t) => t.openUntil ?? t.session?.endsAt ?? null);
  const now = useTicking(ends.some(Boolean));
  const busy = end.isPending || extend.isPending || close.isPending;
  const rowError = end.error ?? extend.error ?? close.error;

  const startEdit = (form: TvForm) => {
    setEditing(form);
    create.reset();
    update.reset();
  };
  const save = (e: SyntheticEvent) => {
    e.preventDefault();
    if (!editing) return;
    const fields = {
      name: editing.name.trim(),
      room: editing.room.trim() || null,
      macs: parseMacs(editing.macs),
      haEntityId: editing.entity.trim() || null,
      openHours: editing.hours ? openHours(editing.start, editing.end, editing.days) : null,
    };
    const done = { onSuccess: () => { setEditing(null); } };
    if (editing.id === undefined || editing.version === undefined) {
      const k = keyed(`create:${JSON.stringify(fields)}`);
      create.mutate({ params: k.params, body: fields }, { ...done, ...k.settled });
      return;
    }
    const id = editing.id;
    const k = keyed(`update:${id}:${String(editing.version)}:${JSON.stringify(fields)}`);
    update.mutate(
      { params: { ...k.params, path: { tvId: id } }, body: { ...fields, expectedVersion: editing.version } },
      {
        ...done,
        ...k.settled,
        onError: () => {
          // A stale version: show the TV as it is now, to check and save again.
          void tvs.refetch().then((r) => {
            const fresh = r.data?.tvs.find((t) => t.id === id);
            if (fresh) setEditing(formOf(fresh));
          });
        },
      },
    );
  };

  const showOpen = () => {
    setPicked(list.map((t) => t.id));
    setDuration('1h');
    open.reset();
    setOpening(true);
  };
  const all = picked.length === list.length;
  const endingKids = list.filter((t) => picked.includes(t.id) && t.session?.watcher === 'child');

  const status = (tv: TvState) => {
    if (tv.prompt) return { meta: `${promptSource(tv.prompt)} · offline until you choose`, body: <WhoIsWatching tv={tv} kids={kids} /> };
    if (tv.session) {
      const s = tv.session;
      const left = s.endsAt ? secondsUntil(s.endsAt, skew, now) : null;
      return {
        meta: `${watcherLabel(s)} watching${s.watcher === 'child' ? ' · their TV time' : ''}`,
        trailing: (
          <>
            {left !== null && <Countdown seconds={left} />}
            <Button variant="danger" disabled={busy} aria-label={`Turn off ${tv.name}`} onClick={() => { const k = keyed(`end:${tv.id}`); end.mutate({ params: { ...k.params, path: { tvId: tv.id } } }, k.settled); }}>
              Turn off
            </Button>
          </>
        ),
      };
    }
    if (tv.open && tv.openClosesAt) {
      const byParent = tv.openReason === 'parent';
      return {
        meta: `Open until ${clockLabel(tv.openClosesAt)}${byParent ? '' : ' (open hours)'} · nobody is asked or charged`,
        trailing: byParent ? (
          <>
            <Button disabled={busy} aria-label={`Keep ${tv.name} open 1 hour longer`} onClick={() => { const k = keyed(`extend:${tv.id}`); extend.mutate({ params: k.params, body: { tvIds: [tv.id] } }, k.settled); }}>
              +1 hr
            </Button>
            <Button variant="danger" disabled={busy} aria-label={`Close ${tv.name} now`} onClick={() => { const k = keyed(`close:${tv.id}`); close.mutate({ params: k.params, body: { tvIds: [tv.id] } }, k.settled); }}>
              Close
            </Button>
          </>
        ) : undefined,
      };
    }
    return { meta: tv.power === 'on' ? 'On, offline' : 'Offline until someone is watching' };
  };

  return (
    <section className="flex flex-col gap-6">
      <PageHeader
        title="TVs"
        description="A TV goes online when someone is watching: a parent, or a child spending their TV time."
        actions={list.length > 0 && <Button variant="primary" onClick={showOpen}>Open TVs…</Button>}
      />
      {rowError && <Notice tone="error">{errorMessage(rowError)}</Notice>}

      <Section title="Now" count={list.length} actions={<Button onClick={() => { startEdit(emptyTv); }}>Add a TV</Button>}>
        {list.length ? (
          <List aria-label="TVs">
            {list.map((tv) => {
              const s = status(tv);
              return (
                <ListRow
                  key={tv.id}
                  stackOnNarrow
                  title={tvTitle(tv)}
                  meta={s.meta}
                  trailing={
                    <>
                      {s.trailing}
                      <Button variant="quiet" aria-label={`Edit ${tv.name}`} onClick={() => { startEdit(formOf(tv)); }}>
                        Edit
                      </Button>
                    </>
                  }
                >
                  {s.body}
                </ListRow>
              );
            })}
          </List>
        ) : (
          tvs.isSuccess && <EmptyState variant="surface" title="No TVs yet" description="Add a TV with its network addresses (MACs) so the router can keep it offline until someone is watching." />
        )}
      </Section>

      {kids.length > 0 && list.length > 0 && (
        <Section title="TV time" description="Earned with rewards, or given under Rewards.">
          <List aria-label="TV time">
            {kids.map((k) => (
              <ListRow key={k.childId} title={k.name} meta={k.hours && !k.hours.open && k.hours.opensAt ? `Screen time starts at ${clockLabel(k.hours.opensAt)}` : undefined}
                trailing={<Tag tone={k.tvSecondsRemaining > 0 ? 'info' : 'neutral'}>{minutesLabel(k.tvSecondsRemaining)}</Tag>} />
            ))}
          </List>
        </Section>
      )}

      {(history.data?.sessions.length ?? 0) > 0 && (
        <Section title="Recently" description="The last sessions, newest first.">
          <List aria-label="Recent TV sessions">
            {history.data?.sessions.slice(0, 10).map((s) => (
              <ListRow
                key={s.id}
                muted={s.endedAt !== null}
                title={`${watcherLabel(s)} on ${s.tvName}`}
                meta={`${clockLabel(s.startedAt)}${s.endedAt ? `–${clockLabel(s.endedAt)}` : ''} · ${s.watcher === 'child' ? `${minutesLabel(s.watchedSeconds)} · ` : ''}${endReasonLabel(s.endReason)}`}
              />
            ))}
          </List>
        </Section>
      )}

      <Sheet
        open={opening}
        onClose={() => { setOpening(false); }}
        title="Open TVs"
        description="Simply online until then: nobody is asked who’s watching and nobody is charged."
        footer={
          <>
            <Button onClick={() => { setOpening(false); }}>Cancel</Button>
            <Button
              variant="primary"
              disabled={open.isPending || picked.length === 0}
              onClick={() => {
                const body = { ...(all ? { all: true } : { tvIds: picked }), duration };
                const k = keyed(`open:${JSON.stringify(body)}`);
                open.mutate({ params: k.params, body }, { onSuccess: () => { setOpening(false); }, ...k.settled });
              }}
            >
              {picked.length === 0 ? 'Choose a TV' : `Open ${all ? 'all TVs' : picked.length === 1 ? (list.find((t) => t.id === picked[0])?.name ?? '1 TV') : `${String(picked.length)} TVs`}`}
            </Button>
          </>
        }
      >
        <div className="grid gap-5">
          {open.error && <Notice tone="error">{errorMessage(open.error)}</Notice>}
          <CheckboxGroup legend="Which TVs">
            {list.map((t) => (
              <Checkbox
                key={t.id}
                label={tvTitle(t)}
                checked={picked.includes(t.id)}
                onChange={() => { setPicked((p) => (p.includes(t.id) ? p.filter((x) => x !== t.id) : [...p, t.id])); }}
              />
            ))}
          </CheckboxGroup>
          <SegmentedControl label="How long" options={durations} value={duration} onChange={setDuration} />
          {endingKids.map((t) => (
            <Notice key={t.id} tone="info">
              {t.session?.childName ?? 'A child'} is watching {t.name}: that stops now, and they keep the rest of their TV time.
            </Notice>
          ))}
        </div>
      </Sheet>

      <Sheet
        open={editing !== null}
        onClose={() => { setEditing(null); }}
        title={editing?.id ? `Edit ${editing.name}` : 'Add a TV'}
        footer={
          <>
            {editing?.id && (
              <Button variant="danger" className="mr-auto" onClick={() => { setRemoving(list.find((t) => t.id === editing.id) ?? null); }}>
                Remove…
              </Button>
            )}
            <Button onClick={() => { setEditing(null); }}>Cancel</Button>
            <Button variant="primary" type="submit" form="tv-form" disabled={create.isPending || update.isPending}>
              Save
            </Button>
          </>
        }
      >
        {editing && (
          <form id="tv-form" onSubmit={save} className="grid gap-5" aria-label={editing.id ? 'Edit TV' : 'Add a TV'}>
            <EditError error={create.error ?? update.error} />
            <div className="grid grid-cols-2 gap-4">
              <Input label="Name" value={editing.name} maxLength={80} required onChange={(e) => { setEditing({ ...editing, name: e.target.value }); }} />
              <Input label="Room" value={editing.room} maxLength={80} hint="Optional." onChange={(e) => { setEditing({ ...editing, room: e.target.value }); }} />
            </div>
            <Textarea
              label="Network addresses (MACs)"
              hint="One per line: the TV’s, and any streaming box on it. Your router lists them."
              rows={3}
              className="font-mono"
              value={editing.macs}
              required
              onChange={(e) => { setEditing({ ...editing, macs: e.target.value }); }}
            />
            <Input
              label="Home Assistant entity"
              hint="Optional, such as media_player.living_room_tv. With it, turning the TV on asks who’s watching, and turning it off ends the session."
              className="font-mono"
              maxLength={255}
              value={editing.entity}
              onChange={(e) => { setEditing({ ...editing, entity: e.target.value }); }}
            />
            <Checkbox
              label="Open hours"
              hint="Every day in this window the TV is simply online, for anyone."
              checked={editing.hours}
              onChange={(e) => { setEditing({ ...editing, hours: e.target.checked }); }}
            />
            {editing.hours && (
              <>
                <div className="grid grid-cols-2 gap-4">
                  <Input label="From" type="time" value={editing.start} required onChange={(e) => { setEditing({ ...editing, start: e.target.value }); }} />
                  <Input label="Until" type="time" value={editing.end} required hint="Earlier than From runs past midnight." onChange={(e) => { setEditing({ ...editing, end: e.target.value }); }} />
                </div>
                <DayPicker label="Days it opens" value={editing.days} onChange={(days) => { setEditing({ ...editing, days }); }} />
                <p className="m-0 text-meta text-muted-foreground">{openHoursLabel(openHours(editing.start, editing.end, editing.days))}</p>
              </>
            )}
          </form>
        )}
      </Sheet>

      <ConfirmDialog
        open={removing !== null}
        title={removing ? `Remove ${removing.name}?` : ''}
        description="Its network addresses are no longer let online. A session on it ends now; its history stays."
        confirmLabel={removing ? `Remove ${removing.name}` : 'Remove'}
        tone="danger"
        pending={remove.isPending}
        error={remove.error ? errorMessage(remove.error) : undefined}
        onCancel={() => { setRemoving(null); remove.reset(); }}
        onConfirm={() => {
          if (!removing) return;
          const k = keyed(`remove:${removing.id}`);
          remove.mutate({ params: { ...k.params, path: { tvId: removing.id } } }, { onSuccess: () => { setRemoving(null); setEditing(null); }, ...k.settled });
        }}
      />
    </section>
  );
}
