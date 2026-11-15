import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ClipboardCheck, House, Sunrise } from 'lucide-react';
import { useState } from 'react';

import { AppFrame, Button, ConnectionBanner, EmptyState, PeopleBoard, PersonCard, TabBar, TaskGroup, TaskRow, TopBar } from '@app/design';
import type { TaskRowCheck } from '@app/design';

import { client, idempotencyKey, type Household, type TodayGroup, type TodayItem } from '../api/client';

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

// The wall display's Today: every child's routines and chores. A tap marks one done; it then
// waits for an adult's approval before the points land.
export function TodayBoard({ household }: { household: Household }) {
  const queryClient = useQueryClient();
  const [local, setLocal] = useState<Local>({});
  const today = useQuery({
    queryKey: todayKey,
    queryFn: async () => {
      const { data } = await client.GET('/today');
      if (!data) throw new Error('Today could not be loaded.');
      return data;
    },
    refetchInterval: 30_000,
  });

  const mark = (group: TodayGroup, item: TodayItem) => async () => {
    setLocal((l) => ({ ...l, [item.id]: 'sending' }));
    const { response } =
      group.kind === 'routine'
        ? await client.POST('/today/routines/{routineId}/steps/{stepId}/check', {
            params: { path: { routineId: group.id, stepId: item.id }, ...idempotencyKey() },
          })
        : await client.POST('/today/chores/{choreId}/complete', {
            params: { path: { choreId: item.id }, ...idempotencyKey() },
          });
    // 409: already done elsewhere, or no longer on today's list; the refreshed day shows which.
    const ok = response.ok || response.status === 409 || response.status === 404;
    await queryClient.invalidateQueries({ queryKey: todayKey });
    setLocal((l) => ({ ...l, [item.id]: ok ? undefined : 'failed' }));
  };

  const now = clock(today.data ? new Date(today.data.serverTime) : new Date(), household.timeZone);
  const updated = today.dataUpdatedAt ? clock(new Date(today.dataUpdatedAt), household.timeZone).time : undefined;
  return (
    <AppFrame
      topBar={<TopBar time={now.time} date={now.date} householdName={household.name} />}
      tabBar={<TabBar items={[{ href: '/display', label: 'Today', icon: House, current: true }]} />}
    >
      {today.isError && (
        <ConnectionBanner
          status="stale"
          lastUpdated={updated}
          actions={<Button variant="secondary" onClick={() => void today.refetch()}>Try again</Button>}
        />
      )}
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
              ))
            )}
          </PersonCard>
        ))}
      </PeopleBoard>
    </AppFrame>
  );
}
