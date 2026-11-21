import { useQueryClient } from '@tanstack/react-query';

import { Button, ChoiceGroup, Countdown, Notice, Panel } from '@app/design';

import { $api, errorMessage, useAttempts, type Child } from '../api/client';
import { minutesLabel, screenState, spendChoices, useServerNow, type ScreenTime } from './screen-time-logic';

const clockTime = new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' });

// The kid view's iPad time: what is left, starting banked time in one tap, and asking a parent
// for more. The iPad app unlocks itself from the same screen time (its allowance), so this only
// asks; it never unlocks anything.
export function KidScreenTime({ kid }: { kid: Child }) {
  const queryClient = useQueryClient();
  const path = { params: { path: { childId: kid.id } } };
  const query = $api.useQuery('get', '/children/{childId}/screen-time', path, { refetchInterval: 30_000 });
  const now = useServerNow(query.data?.serverTime, query.dataUpdatedAt);
  const put = (st: ScreenTime) => { queryClient.setQueryData(['get', '/children/{childId}/screen-time', path], st); };
  const spend = $api.useMutation('post', '/children/{childId}/screen-time/spend', { onSuccess: (d) => { put(d.screenTime); } });
  const ask = $api.useMutation('post', '/children/{childId}/time-requests', { onSuccess: () => void query.refetch() });
  const attempts = useAttempts('kid-screen-time');

  const st = query.data;
  if (!st) {
    return (
      <Panel heading="iPad time" aria-busy={query.isPending || undefined}>
        {query.isError ? <Notice tone="error">{errorMessage(query.error)}</Notice> : <p className="m-0 text-muted-foreground">Checking your time…</p>}
      </Panel>
    );
  }
  const s = screenState(st, now);
  const closed = st.hoursNow && !st.hoursNow.open;
  const opens = closed && st.hoursNow?.opensAt ? clockTime.format(new Date(st.hoursNow.opensAt)) : null;
  const waiting = st.timeRequest?.state === 'pending';
  const start = (minutes: number) => {
    // One key per tap on this amount while the bank is the same: a retry never spends twice.
    const attempt = `spend:${String(minutes)}:${String(st.bankedSeconds)}:${String(st.windows.length)}`;
    spend.mutate({ params: { ...attempts.params(attempt), path: { childId: kid.id } }, body: { minutes } }, { onSettled: attempts.settleIfAnswered(attempt) });
  };
  const askForMore = () => {
    const attempt = `ask:${st.timeRequest?.id ?? 'new'}:${st.timeRequest?.askedAt ?? ''}`;
    ask.mutate({ params: { ...attempts.params(attempt), path: { childId: kid.id } }, body: {} }, { onSettled: attempts.settleIfAnswered(attempt) });
  };
  const choices = spendChoices(s.bankedMinutes).map((m) => ({ value: String(m), label: minutesLabel(m) }));

  return (
    <Panel heading="iPad time" tone={s.runningSeconds > 0 ? 'positive' : 'default'}>
      <div className="flex flex-col gap-5">
        {s.runningSeconds > 0 ? (
          <p className="m-0 flex flex-col gap-1">
            <Countdown size="large" seconds={s.runningSeconds} />
            {s.queuedSeconds > 0 && <span className="text-muted-foreground">then {minutesLabel(Math.ceil(s.queuedSeconds / 60))} more</span>}
          </p>
        ) : (
          <p className="m-0 text-section">{s.bankedMinutes > 0 ? `${minutesLabel(s.bankedMinutes)} saved` : 'No time saved'}</p>
        )}
        {s.runningSeconds > 0 && s.bankedMinutes > 0 && <p className="m-0 text-muted-foreground">{minutesLabel(s.bankedMinutes)} saved for later</p>}
        {(spend.error ?? ask.error) ? <Notice tone="error">{errorMessage(spend.error ?? ask.error)}</Notice> : null}
        {closed ? (
          <Notice tone="info">{opens ? `iPad time opens at ${opens}.` : 'iPad time is closed for today.'}</Notice>
        ) : (
          choices.length > 0 && (
            <ChoiceGroup
              label={s.runningSeconds > 0 ? 'Add time after this' : 'Start iPad time'}
              busy={spend.isPending}
              choices={choices}
              onChoose={(m) => { start(Number(m)); }}
            />
          )
        )}
        {waiting ? (
          <p className="m-0 text-muted-foreground">You asked for more time. A grown-up will answer soon.</p>
        ) : (
          <div>
            <Button variant="secondary" disabled={ask.isPending} onClick={askForMore}>
              Ask for more time
            </Button>
          </div>
        )}
      </div>
    </Panel>
  );
}
