// @dsCard group="Today"
import { AlarmClock, BrushCleaning, ClipboardCheck, Gift, Hourglass, House, Lock, Sunrise } from 'lucide-react';

import {
  AppFrame,
  Button,
  ComingLaterTile,
  ConnectionBanner,
  EmptyState,
  PageHeader,
  PersonCard,
  PointsBadge,
  RewardTile,
  Section,
  TabBar,
  TaskGroup,
  TaskRow,
  TopBar,
} from '../../src';

import { kid } from './fixtures-devices';
import { rewards } from './fixtures-points';
import { today, type TodayItem } from './fixtures-today';

export function Today() {
  return (
    <div className="max-w-md">
      <PersonCard
        name="Ada"
        color="sky"
        points={24}
        completed={1}
        total={3}
        earnedToday={5}
        footer={<a href="#rewards">See rewards</a>}
      >
        <TaskRow title="Brush teeth" points={2} icon={<BrushCleaning />} check={{ status: 'open', checked: false, label: 'Brush teeth' }} />
        <TaskRow title="Make the bed" points={3} check={{ status: 'complete', checked: true, label: 'Make the bed' }} />
        <TaskRow
          title="Pack school bag"
          points={3}
          icon={<AlarmClock />}
          error="Couldn’t save — tap to try again."
          check={{ status: 'failed', label: 'Try again: Pack school bag' }}
        />
      </PersonCard>
    </div>
  );
}

const ada = today.children[0];

function KidFrame({ tab = 'today', children, busy = false }: { tab?: 'today' | 'rewards'; children: React.ReactNode; busy?: boolean }) {
  return (
    <AppFrame
      person={kid.color}
      aria-busy={busy || undefined}
      topBar={<TopBar time="7:42" date="Monday, September 14" householdName={`${kid.name}’s iPad`} />}
      tabBar={
        <TabBar
          items={[
            { href: '#today', label: 'Today', icon: House, current: tab === 'today' },
            { href: '#rewards', label: 'Rewards', icon: Gift, current: tab === 'rewards' },
          ]}
        />
      }
    >
      {children}
    </AppFrame>
  );
}

function kidCheck(item: TodayItem) {
  if (item.state === 'complete' || item.state === 'pending') return { status: 'complete' as const, checked: true, label: item.title };
  if (item.state === 'sending') return { status: 'sending' as const, checked: true, label: item.title };
  if (item.state === 'failed') return { status: 'failed' as const, label: `Try again: ${item.title}` };
  return { status: 'open' as const, checked: false, label: item.title };
}

function KidDay({ failed = false }: { failed?: boolean }) {
  if (!ada) return null;
  return (
    <>
      <PageHeader title={`Good morning, ${kid.name}`} actions={<PointsBadge points={ada.child.points} />} />
      <Section title="Your list">
      {ada.groups.map((group) => (
        <TaskGroup
          key={group.id}
          title={group.title}
          icon={group.kind === 'routine' ? Sunrise : ClipboardCheck}
          status={group.status}
          done={group.done}
          earned={group.done}
          pending={group.pending}
        >
          {group.items.map((item) => {
            const state = failed && item.state === 'sending' ? 'failed' : item.state;
            const shown = { ...item, state };
            return (
              <TaskRow
                key={item.id}
                title={item.title}
                points={item.points}
                status={state === 'failed' ? 'open' : state}
                check={kidCheck(shown)}
                celebrate={item.celebrate}
                message={item.rejectedReason}
                error={state === 'failed' ? 'Couldn’t save — tap to try again.' : undefined}
              />
            );
          })}
        </TaskGroup>
      ))}
      </Section>
      {/* Screen time (banked time, use 30 min, ask for more) arrives with the allowances slice. */}
      <ComingLaterTile title="Screen time" icon={Hourglass} description="Your iPad time will show here." />
    </>
  );
}

/** The kid view at /kid (a web view in the iPad app): the device's own child, tablet-sized, in their colour. */
export function KidToday() {
  return (
    <KidFrame>
      <KidDay />
    </KidFrame>
  );
}

/** Rewards on the kid's own device: the display's Rewards tab without the "Who's choosing?" step. */
export function KidRewards() {
  return (
    <KidFrame tab="rewards">
      <PageHeader title="Rewards" description="Choose one, then an adult says yes." actions={<PointsBadge points={ada?.child.points ?? 0} />} />
      <div className="grid grid-cols-2 gap-4 md:grid-cols-3">
        {rewards.rewards.map((r) => (
          <RewardTile key={r.id} name={r.title} cost={r.cost} balance={ada?.child.points ?? 0} disabled={!r.available} onSelect={() => undefined} />
        ))}
      </div>
    </KidFrame>
  );
}

export function KidLoading() {
  return (
    <KidFrame busy>
      <p className="m-0 text-muted-foreground">Getting {kid.name}’s day…</p>
    </KidFrame>
  );
}

/** Offline: the last day it had, ticks wait, and a tap that failed offers to try again. */
export function KidOffline() {
  return (
    <KidFrame>
      <ConnectionBanner status="offline" lastUpdated="7:38 am" actions={<Button variant="secondary">Try again</Button>} />
      <KidDay failed />
    </KidFrame>
  );
}

export function KidNothingToday() {
  return (
    <KidFrame>
      <PageHeader title={`Good morning, ${kid.name}`} actions={<PointsBadge points={ada?.child.points ?? 0} />} />
      <EmptyState variant="surface" title="Nothing on your list today" description="When a routine or chore is ready, it will show up here." />
    </KidFrame>
  );
}

/** The device session ended: no frame or tabs, nothing to tap but a calm instruction. */
export function KidSessionEnded() {
  return (
    <AppFrame person={kid.color} contentClassName="grid place-content-center">
      <EmptyState icon={Lock} title="Ask an adult to check this iPad" description="It needs to be connected to OpenCozy again." />
    </AppFrame>
  );
}

/** /device/expired: shown for a moment while the app takes over, so it stays quiet. */
export function DeviceExpired() {
  return (
    <AppFrame contentClassName="grid place-content-center">
      <p className="m-0 text-muted-foreground">One moment…</p>
    </AppFrame>
  );
}
