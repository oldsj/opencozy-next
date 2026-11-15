// @dsCard group="Today"
import { CalendarDays, ClipboardCheck, Gift, House, Sunrise } from 'lucide-react';

import { AppFrame, Button, ConnectionBanner, EmptyState, PeopleBoard, PersonCard, StatusPill, TabBar, TaskGroup, TaskRow, TopBar } from '../../src';
import type { TaskRowCheck } from '../../src';

import { today, type TodayItem } from './fixtures-today';

export function ThreePeople() {
  return (
    <PeopleBoard>
      <PersonCard name="Ada" color="sky" points={24} completed={1} total={2}>
        <TaskGroup title="Morning" icon={Sunrise} status="3 points">
          <TaskRow title="Make the bed" check={{ status: 'complete', checked: true, label: 'Make the bed' }} />
          <TaskRow title="Pack school bag" check={{ status: 'open', checked: false, label: 'Pack school bag' }} />
        </TaskGroup>
      </PersonCard>
      <PersonCard name="Ezra" color="sage" points={18} completed={0} total={1}>
        <TaskGroup title="Chores" icon={ClipboardCheck}>
          <TaskRow
            title="Feed the cat"
            points={4}
            error="Couldn’t save — tap to try again."
            check={{ status: 'failed', label: 'Try again: Feed the cat' }}
          />
        </TaskGroup>
      </PersonCard>
      <PersonCard name="Noor" color="lilac" points={6} completed={0} total={0}>
        <EmptyState title="Nothing on the list today" description="When a routine or chore is ready, it will appear here." />
      </PersonCard>
    </PeopleBoard>
  );
}

function checkFor(item: TodayItem): TaskRowCheck {
  switch (item.state) {
    case 'complete':
    case 'pending':
      return { status: 'complete', checked: true, label: item.title };
    case 'sending':
      return { status: 'sending', checked: true, label: item.title };
    case 'failed':
      return { status: 'failed', label: `Try again: ${item.title}` };
    default:
      return { status: 'open', checked: false, label: item.title };
  }
}

/**
 * The wall display's Today, full screen (view it with ?surface=kiosk). It is built only from
 * `today` (fixtures-today.ts, the GET /api/today shape), so wiring it to the API means swapping that data.
 */
export function KioskToday() {
  return (
    <AppFrame
      topBar={<TopBar time="7:42" date="Monday, September 14" householdName="Maple House" status={<StatusPill status="reconnecting" />} />}
      tabBar={
        <TabBar
          items={[
            { href: '#today', label: 'Today', icon: House, current: true },
            { href: '#rewards', label: 'Rewards', icon: Gift },
            { href: '#calendar', label: 'Calendar', icon: CalendarDays },
          ]}
        />
      }
    >
      <ConnectionBanner status="stale" lastUpdated="7:38 am" actions={<Button variant="secondary">Try again</Button>} />
      <PeopleBoard>
        {today.children.map(({ child, groups }) => (
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
                      status={item.state === 'failed' ? 'open' : item.state}
                      check={checkFor(item)}
                      celebrate={item.celebrate}
                      message={item.rejectedReason}
                      error={item.state === 'failed' ? 'Couldn’t save — tap to try again.' : undefined}
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
