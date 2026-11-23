// @dsCard group="Today"
import { AlarmClock, BrushCleaning, ClipboardCheck, Gift, House, Tablet, Tv, Lock, Sunrise } from 'lucide-react';

import {
  AppFrame,
  Button,
  ConnectionBanner,
  Countdown,
  EmptyState,
  Notice,
  PageHeader,
  PersonCard,
  PointsBadge,
  RewardTile,
  Section,
  TabBar,
  TaskGroup,
  TaskRow,
  TopBar,
  Widget,
  profileClass,
} from '../../src';

import { kid } from './fixtures-devices';
import { kidTvStates, type KidTv } from './fixtures-tv';
import { SPEND_MINUTES, timeStates, type PersonalTime } from './fixtures-screentime';
import { rewards } from './fixtures-points';
import { today, type TodayItem } from './fixtures-today';

export function Today() {
  return (
    <div className="max-w-md">
      <PersonCard
        name="Abby"
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

const abby = today.children[0];

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
  if (!abby) return null;
  return (
    <>
      <PageHeader title={`Good morning, ${kid.name}`} actions={<PointsBadge points={abby.child.points} />} />
      <Section title="Your list">
      {abby.groups.map((group) => (
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
      <KidTime time={timeStates.locked} />
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
      <PageHeader title="Rewards" description="Choose one, then an adult says yes." actions={<PointsBadge points={abby?.child.points ?? 0} />} />
      <div className="grid grid-cols-2 gap-4 md:grid-cols-3">
        {rewards.rewards.map((r) => (
          <RewardTile key={r.id} name={r.title} cost={r.cost} balance={abby?.child.points ?? 0} disabled={!r.available} onSelect={() => undefined} />
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
      <PageHeader title={`Good morning, ${kid.name}`} actions={<PointsBadge points={abby?.child.points ?? 0} />} />
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

function minutes(seconds: number): string {
  const m = Math.floor(seconds / 60);
  return m >= 60 ? `${String(Math.floor(m / 60))} h ${String(m % 60)} min` : `${String(m)} min`;
}

/**
 * The child's iPad time on /kid: the window running now, the time saved, one button to use some,
 * and Ask for more time. Each refusal says why in words a child can act on.
 */
function KidTime({ time }: { time: PersonalTime }) {
  const saved = time.secondsRemaining;
  const useMinutes = Math.min(SPEND_MINUTES, Math.floor(saved / 60));
  const closed = time.allowedHours !== null && !time.allowedHours.open;
  const request = time.timeRequest;
  const pending = request?.state === 'pending';
  return (
    <Widget title="iPad time" icon={Tablet} footer={time.allowedHours?.open ? `Screen time until ${time.allowedHours.closesAt ?? ''} today` : undefined}>
      <div className="grid gap-4">
        {time.unlockedUntil !== null && (
          <div className="grid gap-1">
            <Countdown size="large" seconds={time.secondsLeftNow} />
            <p className="m-0 text-muted-foreground">Unlocked until {time.unlockedUntil}</p>
          </div>
        )}
        <p className="m-0 text-section">{saved > 0 ? `${minutes(saved)} saved` : 'No iPad time saved'}</p>
        {closed && <Notice tone="info">Screen time starts at {time.allowedHours?.opensAt}.</Notice>}
        {request?.state === 'answered' && request.choice !== 'no' && (
          <Notice tone="success" title="A parent said yes!">
            iPad time until {request.grantedUntil}.
          </Notice>
        )}
        {request?.state === 'answered' && request.choice === 'no' && <Notice tone="attention" title="Not right now">You can ask again in a few minutes.</Notice>}
        <div className="flex flex-wrap gap-3">
          {useMinutes > 0 && (
            <Button variant="primary" disabled={closed}>
              {time.unlockedUntil === null ? `Use ${String(useMinutes)} min` : `Add ${String(useMinutes)} min`}
            </Button>
          )}
          <Button disabled={pending}>{pending ? `Asked at ${request.requestedAt} · waiting` : 'Ask for more time'}</Button>
        </div>
        {saved === 0 && !request && <p className="m-0 text-muted-foreground">Earn time with chores and rewards, or ask a parent.</p>}
      </div>
    </Widget>
  );
}

/** Every state of the kid's iPad time, side by side. */
export function KidTimeStates() {
  return (
    <div className={`${profileClass(kid.color)} grid gap-4 md:grid-cols-2`}>
      <KidTime time={timeStates.locked} />
      <KidTime time={timeStates.unlocked} />
      <KidTime time={timeStates.outsideHours} />
      <KidTime time={timeStates.nothingBanked} />
      <KidTime time={timeStates.asked} />
      <KidTime time={timeStates.yes} />
      <KidTime time={timeStates.no} />
    </div>
  );
}

/**
 * The child's TV time on /kid: the balance, a TV asking "Who's watching?" that they can start from
 * here, the session running now (with Done watching), and why not when they can't.
 */
function KidTvTime({ tv }: { tv: KidTv }) {
  const watching = tv.watching;
  const waiting = tv.waiting[0];
  return (
    <Widget title="TV time" icon={Tv}>
      <div className="grid gap-4">
        {watching ? (
          <>
            <div className="grid gap-1">
              <Countdown size="large" seconds={watching.secondsRemaining} />
              <p className="m-0 text-muted-foreground">
                On {watching.tvName}
                {watching.endsWithHours ? ' · screen time ends then' : ''}
              </p>
            </div>
            <div>
              <Button>Done watching</Button>
            </div>
          </>
        ) : (
          <p className="m-0 text-section">{tv.secondsRemaining > 0 ? `${minutes(tv.secondsRemaining)} of TV time` : 'No TV time'}</p>
        )}
        {!watching && !tv.hoursOpen && <Notice tone="info">Screen time starts at {tv.opensAt}.</Notice>}
        {!watching && waiting && tv.hoursOpen && tv.secondsRemaining > 0 && (
          <Notice tone="success" title={`${waiting.tvName} is on`}>
            <Button variant="primary">Watch on {waiting.tvName}</Button>
          </Notice>
        )}
        {!watching && tv.secondsRemaining === 0 && <p className="m-0 text-muted-foreground">Earn TV time with chores and rewards, or ask a parent.</p>}
      </div>
    </Widget>
  );
}

/** Every state of the kid's TV time, side by side. */
export function KidTvStates() {
  return (
    <div className={`${profileClass(kid.color)} grid gap-4 md:grid-cols-2`}>
      <KidTvTime tv={kidTvStates.idle} />
      <KidTvTime tv={kidTvStates.waiting} />
      <KidTvTime tv={kidTvStates.watching} />
      <KidTvTime tv={kidTvStates.endsWithHours} />
      <KidTvTime tv={kidTvStates.outsideHours} />
      <KidTvTime tv={kidTvStates.none} />
    </div>
  );
}
