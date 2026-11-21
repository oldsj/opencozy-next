import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type ReactNode } from 'react';

import { Button, Countdown, EmptyState, List, ListRow, Notice, Section } from '@app/design';

import { client, errorMessage, useAttempts } from '../api/client';
import { clockLabel, clockSkew, minutesLabel, secondsUntil, tvTitle, TV_REFRESH_MS, useTicking } from './tv';

export const displayTvsKey = ['display', 'tvs'] as const;

/**
 * The wall display's TVs, or null when this display may not see them (no `tv` scope): then the
 * board shows no TV tab at all.
 */
export function useDisplayTvs(enabled: boolean) {
  return useQuery({
    queryKey: displayTvsKey,
    enabled,
    queryFn: async () => {
      const { data, response } = await client.GET('/tvs/display');
      if (response.status === 403) return null;
      if (!data) throw new Error('The TVs could not be loaded.');
      return { ...data, skew: clockSkew(data.serverTime, Date.now()) };
    },
    // A display without TVs asks once; one with them keeps the countdowns fresh.
    refetchInterval: (q) => (q.state.data === null ? false : TV_REFRESH_MS),
  });
}

// The wall display's TV tab: each TV and who is on it. A child asks for a TV here (a parent then
// answers "Who's watching?" on their phone, or the child on their own device) and says when
// they're done. Nothing here spends anyone's time.
export function DisplayTvs({ tvs }: { tvs: NonNullable<ReturnType<typeof useDisplayTvs>['data']> }) {
  const queryClient = useQueryClient();
  const attempts = useAttempts();
  const [busy, setBusy] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const now = useTicking(tvs.tvs.some((t) => t.session?.endsAt));

  const act = async (tvId: string, what: 'ask' | 'done') => {
    const attempt = `${what}:${tvId}`;
    setBusy(attempt);
    setProblem(null);
    try {
      const params = { ...attempts.params(attempt), path: { tvId } };
      const { error, response } = what === 'ask' ? await client.POST('/tvs/{tvId}/ask', { params }) : await client.POST('/tvs/{tvId}/done', { params });
      if (response.status < 500) attempts.settle(attempt);
      if (error) setProblem(errorMessage(error));
    } catch {
      setProblem('That didn’t go through. Tap again to try again.');
    } finally {
      setBusy(null);
      await queryClient.invalidateQueries({ queryKey: displayTvsKey }).catch(() => undefined);
    }
  };

  if (!tvs.tvs.length) {
    return <EmptyState variant="surface" title="No TVs yet" description="A parent adds TVs on their phone, under TVs." />;
  }
  return (
    <Section title="TVs" hideTitle>
      {problem && <Notice tone="error">{problem}</Notice>}
      <List aria-label="TVs">
        {tvs.tvs.map((tv) => {
          const s = tv.session;
          let meta: string;
          let action: ReactNode = null;
          if (s) {
            meta = s.watcher === 'parents' ? 'Parents are watching' : `${s.childName ?? 'A child'} is watching`;
            action = (
              <>
                {s.endsAt && <Countdown size="large" seconds={secondsUntil(s.endsAt, tvs.skew, now)} />}
                <Button variant="secondary" disabled={busy !== null} onClick={() => void act(tv.id, 'done')}>
                  Done watching
                </Button>
              </>
            );
          } else if (tv.openClosesAt) {
            meta = `Open for everyone until ${clockLabel(tv.openClosesAt)}`;
          } else if (tv.asking) {
            meta = 'Asking who’s watching. Answer on your iPad, or ask a parent.';
          } else {
            meta = 'Off';
            action = (
              <Button variant="primary" disabled={busy !== null} onClick={() => void act(tv.id, 'ask')}>
                Ask for this TV
              </Button>
            );
          }
          return <ListRow key={tv.id} stackOnNarrow title={tvTitle(tv)} meta={meta} trailing={action} />;
        })}
      </List>
    </Section>
  );
}

// A child's TV time on their own device: what's left, a TV asking that they can start on, and
// the one on now with its countdown and "I'm done". Hidden for a child with no TV time and no TV
// asking.
export function KidTv({ childId }: { childId: string }) {
  const queryClient = useQueryClient();
  const attempts = useAttempts();
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const key = ['kid', 'tv', childId] as const;
  const tv = useQuery({
    queryKey: key,
    queryFn: async () => {
      const { data, response } = await client.GET('/children/{childId}/tv', { params: { path: { childId } } });
      if (response.status === 403 || response.status === 404) return null;
      if (!data) throw new Error('TV time could not be loaded.');
      return { ...data, skew: clockSkew(data.serverTime, Date.now()) };
    },
    refetchInterval: TV_REFRESH_MS,
  });
  const now = useTicking(Boolean(tv.data?.watching));
  const d = tv.data;
  if (!d || (d.secondsRemaining <= 0 && !d.watching && !d.waiting.length)) return null;

  const send = async (attempt: string, call: (p: { header: { 'Idempotency-Key': string } }) => Promise<{ error?: unknown; response: Response }>) => {
    setBusy(true);
    setProblem(null);
    try {
      const { error, response } = await call(attempts.params(attempt));
      if (response.status < 500) attempts.settle(attempt);
      if (error) setProblem(errorMessage(error));
    } catch {
      setProblem('That didn’t go through. Tap again to try again.');
    } finally {
      setBusy(false);
      await queryClient.invalidateQueries({ queryKey: key }).catch(() => undefined);
    }
  };
  const closed = d.hours && !d.hours.open && d.hours.opensAt ? clockLabel(d.hours.opensAt) : null;

  if (d.watching) {
    const w = d.watching;
    const left = secondsUntil(w.endsAt, d.skew, now);
    return (
      <Section title={`Watching ${w.tvName}`} description={w.endsWithHours ? 'Until screen time ends today.' : undefined}>
        {problem && <Notice tone="error">{problem}</Notice>}
        <div className="flex flex-wrap items-center gap-4">
          <Countdown size="large" seconds={left} />
          <Button
            disabled={busy}
            onClick={() => void send(`done:${w.tvId}`, (p) => client.POST('/tvs/{tvId}/done', { params: { ...p, path: { tvId: w.tvId } } }))}
          >
            I’m done
          </Button>
        </div>
      </Section>
    );
  }
  return (
    <Section title="TV time" description={closed ? `You have ${minutesLabel(d.secondsRemaining)}. Screen time starts at ${closed}.` : `You have ${minutesLabel(d.secondsRemaining)}.`}>
      {problem && <Notice tone="error">{problem}</Notice>}
      {d.waiting.length > 0 && d.secondsRemaining > 0 && !closed ? (
        <div className="flex flex-wrap gap-3">
          {d.waiting.map((t) => (
            <Button
              key={t.tvId}
              variant="primary"
              disabled={busy}
              onClick={() => void send(`watch:${t.tvId}`, (p) => client.POST('/children/{childId}/tv/watch', { params: { ...p, path: { childId } }, body: { tvId: t.tvId } }))}
            >
              Watch {tvTitle({ name: t.tvName, room: t.room })}
            </Button>
          ))}
        </div>
      ) : (
        <p className="m-0 text-muted-foreground">Turn on a TV, then start your time here.</p>
      )}
    </Section>
  );
}
