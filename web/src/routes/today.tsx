import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ClipboardCheck, Gift, Hourglass, House, Sunrise, Tv } from 'lucide-react';
import { useState } from 'react';

import {
  AppFrame,
  Button,
  ComingLaterTile,
  ConnectionBanner,
  EmptyState,
  PageHeader,
  PeopleBoard,
  PersonCard,
  PointsBadge,
  Section,
  TabBar,
  TaskGroup,
  TaskRow,
  TopBar,
} from '@app/design';
import type { TaskRowCheck } from '@app/design';

import { client, useAttempts, type Child, type TodayGroup, type TodayItem } from '../api/client';
import { DisplayRewards } from './display-rewards';
import { DisplayTvs, KidTv, useDisplayTvs } from './display-tv';

export const todayKey = ['display', 'today'] as const;

// What a tap is doing right now, by item id: saving, or it failed and can be tried again.
type Local = Partial<Record<string, 'sending' | 'failed'>>;

function checkFor(item: TodayItem, local: 'sending' | 'failed' | undefined, onClick: () => void): TaskRowCheck {
  if (local === 'sending') return { status: 'sending', checked: true, label: item.title };
  if (local === 'failed') return { status: 'failed', label: `Try again: ${item.title}`, onClick };
  if (item.state === 'open') return { status: 'open', checked: false, label: item.title, onClick };
  return { status: 'complete', checked: true, label: item.title };
}

function clock(at: Date, timeZone: string) {
  return {
    time: new Intl.DateTimeFormat(undefined, { timeZone, hour: 'numeric', minute: '2-digit' }).format(at),
    date: new Intl.DateTimeFormat(undefined, { timeZone, weekday: 'long', month: 'long', day: 'numeric' }).format(at),
  };
}

function greeting(at: Date, timeZone: string) {
  const hour = Number(new Intl.DateTimeFormat('en-GB', { timeZone, hour: 'numeric', hourCycle: 'h23' }).format(at));
  if (hour < 12) return 'Good morning';
  return hour < 18 ? 'Good afternoon' : 'Good evening';
}

// The wall display's Today: every child's routines and chores. A tap marks one done; it then
// waits for an adult's approval before the points land.
//
// With `kid`, the same board on a child's own device (/kid): only their day, in their colour,
// and Rewards without "Who's choosing?". `onSignedOut` is called when the session has ended.
export function TodayBoard({
  household,
  kid,
  onSignedOut,
}: {
  household: { name: string; timeZone: string };
  kid?: Child | undefined;
  onSignedOut?: (() => void) | undefined;
}) {
  const queryClient = useQueryClient();
  const [local, setLocal] = useState<Local>({});
  const attempts = useAttempts();
  type Tab = 'today' | 'rewards' | 'tv';
  const [tab, setTab] = useState<Tab>(window.location.hash === '#rewards' ? 'rewards' : window.location.hash === '#tv' ? 'tv' : 'today');
  const show = (t: Tab) => (e: { preventDefault: () => void }) => {
    e.preventDefault();
    window.history.replaceState(null, '', t === 'today' ? '#' : `#${t}`);
    setTab(t);
  };
  // The wall display's TV tab, only for a display allowed to see TVs (its `tv` scope).
  const scopes = useQuery({
    queryKey: ['display', 'session'],
    enabled: !kid,
    queryFn: async () => (await client.GET('/display/session')).data?.scopes ?? [],
    staleTime: 5 * 60_000,
  }).data;
  const tvs = useDisplayTvs(!kid && (scopes?.includes('tv') ?? false)).data ?? null;
  const today = useQuery({
    queryKey: todayKey,
    queryFn: async () => {
      const { data, response } = await client.GET('/today');
      if (response.status === 401) onSignedOut?.();
      if (!data) throw new Error('Today could not be loaded.');
      return data;
    },
    refetchInterval: 30_000,
  });

  // A failed tap (no answer, or a server error) shows "try again"; the retry reuses its key.
  const mark = (group: TodayGroup, item: TodayItem) => async () => {
    setLocal((l) => ({ ...l, [item.id]: 'sending' }));
    let ok = false;
    try {
      const params = attempts.params(item.id);
      const { response } =
        group.kind === 'routine'
          ? await client.POST('/today/routines/{routineId}/steps/{stepId}/check', {
              params: { path: { routineId: group.id, stepId: item.id }, ...params },
            })
          : await client.POST('/today/chores/{choreId}/complete', {
              params: { path: { choreId: item.id }, ...params },
            });
      // 409: already done elsewhere, or no longer on today's list; the refreshed day shows which.
      ok = response.ok || response.status === 409 || response.status === 404;
      if (response.status < 500) attempts.settle(item.id);
    } catch {
      ok = false;
    }
    setLocal((l) => ({ ...l, [item.id]: ok ? undefined : 'failed' }));
    await queryClient.invalidateQueries({ queryKey: todayKey }).catch(() => undefined);
  };

  const at = today.data ? new Date(today.data.serverTime) : new Date();
  const now = clock(at, household.timeZone);
  const groupsOf = (groups: TodayGroup[]) =>
    groups.map((group) => (
      <TaskGroup
        key={group.id}
        title={group.title}
        icon={group.kind === 'routine' ? Sunrise : ClipboardCheck}
        status={group.status}
        done={group.done}
        earned={group.done}
        pending={group.pending}
      >
        {group.items.map((item) => (
          <TaskRow
            key={item.id}
            title={item.title}
            points={item.points}
            status={item.state}
            check={checkFor(item, local[item.id], () => void mark(group, item)())}
            message={item.rejectedReason}
            error={local[item.id] === 'failed' ? 'Couldn’t save — tap to try again.' : undefined}
          />
        ))}
      </TaskGroup>
    ));
  const mine = kid ? today.data?.children.find((c) => c.child.id === kid.id) : undefined;
  const updated = today.dataUpdatedAt ? clock(new Date(today.dataUpdatedAt), household.timeZone).time : undefined;
  return (
    <AppFrame
      person={kid?.color}
      aria-busy={today.isPending || undefined}
      topBar={<TopBar time={now.time} date={now.date} householdName={kid ? `${kid.name}’s day` : household.name} />}
      tabBar={
        <TabBar
          items={[
            { href: '#today', label: 'Today', icon: House, current: tab === 'today', onClick: show('today') },
            { href: '#rewards', label: 'Rewards', icon: Gift, current: tab === 'rewards', onClick: show('rewards') },
            ...(tvs ? [{ href: '#tv', label: 'TV', icon: Tv, current: tab === 'tv', onClick: show('tv') }] : []),
          ]}
        />
      }
    >
      {today.isError && (
        <ConnectionBanner
          status="stale"
          lastUpdated={updated}
          actions={<Button variant="secondary" onClick={() => void today.refetch()}>Try again</Button>}
        />
      )}
      {tab === 'tv' && tvs ? (
        <DisplayTvs tvs={tvs} />
      ) : tab === 'rewards' ? (
        <DisplayRewards today={today.data} todayKey={todayKey} childId={kid?.id} />
      ) : kid ? (
        <>
          <PageHeader title={`${greeting(at, household.timeZone)}, ${kid.name}`} actions={<PointsBadge points={mine?.child.points ?? 0} />} />
          {today.isPending ? (
            <p className="m-0 text-muted-foreground">Getting {kid.name}’s day…</p>
          ) : mine && mine.groups.length > 0 ? (
            <Section title="Your list">{groupsOf(mine.groups)}</Section>
          ) : (
            <EmptyState variant="surface" title="Nothing on your list today" description="When a routine or chore is ready, it will show up here." />
          )}
          <KidTv childId={kid.id} />
          {/* Screen time (banked time, use 30 min, ask for more) arrives with the allowances slice. */}
          <ComingLaterTile title="Screen time" icon={Hourglass} description="Your iPad time will show here." />
        </>
      ) : (
      <PeopleBoard>
        {today.data?.children.map(({ child, groups }) => (
          <PersonCard
            key={child.id}
            name={child.name}
            color={child.color}
            points={child.points}
            earnedToday={child.earnedToday}
            completed={child.completed}
            total={child.total}
          >
            {groups.length === 0 ? (
              <EmptyState title="Nothing on the list today" description="When a routine or chore is ready, it will appear here." />
            ) : (
              groupsOf(groups)
            )}
          </PersonCard>
        ))}
      </PeopleBoard>
      )}
    </AppFrame>
  );
}
