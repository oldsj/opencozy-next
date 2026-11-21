import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { Button, ConfirmDialog, List, ListRow, Notice, profileColors, Section, Sheet, Tag } from '@app/design';

import { $api, errorMessage, idempotencyKey, useAttempts } from '../api/client';
import type { components } from '../api/schema.gen';

type GoogleConnection = components['schemas']['GoogleConnection'];
type Color = components['schemas']['CalendarSource']['color'];

/** Where Google sends an adult back: /calendars?google=<outcome>. */
export const googleOutcomes = ['connected', 'denied', 'expired', 'failed', 'scope_missing', 'too_many'] as const;
export type GoogleOutcome = (typeof googleOutcomes)[number];

// One sentence for each way back from Google but success.
export const googleOutcomeText: Record<Exclude<GoogleOutcome, 'connected'>, string> = {
  denied: 'Google wasn’t connected: you chose not to allow it.',
  expired: 'That sign-in took too long. Try connecting again.',
  failed: 'Google couldn’t be connected. Try again in a minute.',
  scope_missing: 'Google was connected without calendar access. Connect again and tick “See your calendars”.',
  too_many: 'This household has 10 Google accounts. Disconnect one to add another.',
};

/** The first colour no calendar uses yet, else the next in turn. */
export function nextColor(used: Color[]): Color {
  return profileColors.find((c) => !used.includes(c)) ?? profileColors[used.length % profileColors.length] ?? 'sky';
}

/** Starts a connect (or a sign-in again: Google answers with the same account) and leaves for Google. */
export function useConnectGoogle() {
  const connect = $api.useMutation('post', '/calendar/google/connect', {
    onSuccess: (data) => { window.location.assign(data.authorizeUrl); },
  });
  return { start: () => { connect.mutate({ params: idempotencyKey() }); }, pending: connect.isPending || connect.isSuccess, error: connect.error };
}

const refreshKeys = [['get', '/calendars'], ['get', '/calendar'], ['get', '/calendar/google/connections']];

/**
 * The household's Google accounts: choose each one's calendars, sign in again, or disconnect.
 * `choose` opens the picker for one account (after coming back from Google, the new one).
 */
export function GoogleAccounts({ choose, onChoose, usedColors }: { choose: string | null; onChoose: (id: string | null) => void; usedColors: Color[] }) {
  const queryClient = useQueryClient();
  const refresh = () => Promise.all(refreshKeys.map((queryKey) => queryClient.invalidateQueries({ queryKey })));
  const accounts = $api.useQuery('get', '/calendar/google/connections');
  const connect = useConnectGoogle();
  const disconnect = $api.useMutation('delete', '/calendar/google/connections/{connectionId}', { onSuccess: refresh });
  const [leaving, setLeaving] = useState<GoogleConnection | null>(null);

  const list = accounts.data?.connections ?? [];
  const picking = list.find((a) => a.id === choose) ?? null;
  if (!accounts.data?.enabled) return null;

  return (
    <>
      <Section title="Google accounts" {...(list.length > 0 ? { count: list.length } : {})}>
        {list.length > 0 && (
          <List aria-label="Google accounts">
            {list.map((a) => (
              <ListRow
                key={a.id}
                stackOnNarrow
                title={a.email}
                meta={
                  a.needsReconnect
                    ? 'Google stopped accepting this sign-in. Its calendars keep their events until you sign in again.'
                    : `${String(a.calendars)} ${a.calendars === 1 ? 'calendar' : 'calendars'} followed`
                }
                trailing={
                  <>
                    {a.needsReconnect ? (
                      <Button variant="primary" aria-label={`Sign in again: ${a.email}`} disabled={connect.pending} onClick={connect.start}>
                        Sign in again
                      </Button>
                    ) : (
                      <Button aria-label={`Choose calendars from ${a.email}`} onClick={() => { onChoose(a.id); }}>
                        Choose calendars
                      </Button>
                    )}
                    <Button variant="quiet" aria-label={`Disconnect ${a.email}`} onClick={() => { disconnect.reset(); setLeaving(a); }}>
                      Disconnect…
                    </Button>
                  </>
                }
              />
            ))}
          </List>
        )}
        <div className={list.length > 0 ? 'mt-4' : undefined}>
          <Button disabled={connect.pending} onClick={connect.start}>
            Connect Google
          </Button>
        </div>
        {connect.error && <Notice tone="error">{errorMessage(connect.error)}</Notice>}
      </Section>

      {picking && <GoogleCalendarPicker account={picking} usedColors={usedColors} onClose={() => { onChoose(null); }} />}

      <ConfirmDialog
        open={leaving !== null}
        title={leaving ? `Disconnect ${leaving.email}?` : ''}
        description="Its calendars leave OpenCozy, with their events and who they were for. Connect it again to follow them again."
        confirmLabel="Disconnect"
        tone="danger"
        pending={disconnect.isPending}
        error={disconnect.error ? errorMessage(disconnect.error) : undefined}
        onCancel={() => { setLeaving(null); }}
        onConfirm={() => {
          if (!leaving) return;
          disconnect.mutate(
            { params: { ...idempotencyKey(), path: { connectionId: leaving.id }, query: { expectedVersion: leaving.version } } },
            { onSuccess: () => { setLeaving(null); } },
          );
        }}
      />
    </>
  );
}

/** One tap follows a calendar: on the wall display, in the next free colour. Edit changes both. */
function GoogleCalendarPicker({ account, usedColors, onClose }: { account: GoogleConnection; usedColors: Color[]; onClose: () => void }) {
  const queryClient = useQueryClient();
  const choices = $api.useQuery('get', '/calendar/google/connections/{connectionId}/calendars', { params: { path: { connectionId: account.id } } });
  const follow = $api.useMutation('post', '/calendar/google/connections/{connectionId}/calendars', {
    onSuccess: () => Promise.all([...refreshKeys, ['get', '/calendar/google/connections/{connectionId}/calendars']].map((queryKey) => queryClient.invalidateQueries({ queryKey }))),
  });
  const attempts = useAttempts();

  const add = (googleId: string, name: string) => {
    const body = { googleId, name: name.trim().slice(0, 80) || 'Google calendar', color: nextColor(usedColors), showOnDisplay: true };
    const attempt = JSON.stringify([account.id, googleId]);
    follow.mutate({ params: { ...attempts.params(attempt), path: { connectionId: account.id } }, body }, { onSettled: attempts.settleIfAnswered(attempt) });
  };

  return (
    <Sheet
      open
      onClose={onClose}
      title="Choose calendars"
      description={`From ${account.email}. Followed calendars show on the wall display; change that from Edit.`}
      footer={<Button onClick={onClose}>Done</Button>}
    >
      {choices.error && <Notice tone="error">{errorMessage(choices.error)}</Notice>}
      {follow.error && <Notice tone="error">{errorMessage(follow.error)}</Notice>}
      {choices.isPending && <p className="m-0 text-muted-foreground">Asking Google…</p>}
      {choices.data && (
        <List aria-label="Google calendars">
          {choices.data.calendars.map((c) => (
            <ListRow
              key={c.googleId}
              title={c.name}
              meta={c.primary ? 'Main calendar' : undefined}
              trailing={
                c.calendarId ? (
                  <Tag tone="positive">Following</Tag>
                ) : (
                  <Button aria-label={`Follow ${c.name}`} disabled={follow.isPending} onClick={() => { add(c.googleId, c.name); }}>
                    Follow
                  </Button>
                )
              }
            />
          ))}
        </List>
      )}
    </Sheet>
  );
}
