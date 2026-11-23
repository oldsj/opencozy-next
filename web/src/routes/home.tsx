import { Link } from '@tanstack/react-router';
import { Inbox, Tv, Users } from 'lucide-react';

import { Countdown, EmptyState, List, ListRow, Notice, PageHeader, PointsBadge, ProfileAvatar, ProgressBar, Section, Tag } from '@app/design';

import { $api, errorMessage, type Child, type Me } from '../api/client';
import { RouterButtonLink } from '../link';
import { useWaiting } from '../shell';
import { minutesLabel as screenMinutes, screenState, useServerNow } from './screen-time-logic';
import { clockLabel, minutesLabel, secondsUntil, tvTitle, useTicking, watcherLabel, type TvState } from './tv';
import { useTvs } from './tvs';

const longDate = (at: Date, timeZone: string) => new Intl.DateTimeFormat(undefined, { timeZone, weekday: 'long', month: 'long', day: 'numeric' }).format(at);

// The adult's overview: what waits for them, how each child's day is going, and the screens now.
export function HomePage({ me }: { me: Me }) {
  const waiting = useWaiting();
  const today = $api.useQuery('get', '/today', {}, { refetchInterval: 30_000 });
  const children = $api.useQuery('get', '/children');
  const live = children.data?.items.filter((c) => !c.archived) ?? [];
  const { tvs, skew } = useTvs();
  const tvList = tvs.data?.tvs ?? [];
  const date = longDate(today.data ? new Date(today.data.serverTime) : new Date(), me.household.timeZone);

  return (
    <>
      <PageHeader
        title={me.household.name}
        description={`${date}. What’s waiting, and how the day is going.`}
        actions={waiting ? <RouterButtonLink to="/inbox" variant="primary">Open the inbox</RouterButtonLink> : undefined}
      />
      <Section title="Waiting for you">
        {waiting ? (
          <List>
            <ListRow
              leading={<Inbox aria-hidden="true" size={22} />}
              title={<Link to="/inbox">{waiting === 1 ? '1 thing to look at' : `${String(waiting)} things to look at`}</Link>}
              meta="Chores and routines to approve, rewards, requests for more time, TVs asking who’s watching."
            />
          </List>
        ) : (
          waiting === 0 && (
            <EmptyState variant="surface" icon={Inbox} title="Nothing waiting" description="When a child finishes something, asks for more time, or a TV asks who’s watching, it shows here." />
          )
        )}
      </Section>

      <Section title="Today" count={today.data?.children.length}>
        {today.isError && <Notice tone="error">{errorMessage(today.error)}</Notice>}
        {today.data &&
          (today.data.children.length ? (
            <List aria-label="Today’s progress">
              {today.data.children.map(({ child }) => (
                <ListRow
                  key={child.id}
                  leading={<ProfileAvatar name={child.name} color={child.color} size="small" aria-hidden="true" />}
                  title={
                    <Link to="/children/$childId/points" params={{ childId: child.id }}>
                      {child.name}
                    </Link>
                  }
                  meta={[child.total ? `${String(child.completed)} of ${String(child.total)} done` : 'Nothing on the list today', child.earnedToday ? `+${String(child.earnedToday)} today` : null]
                    .filter(Boolean)
                    .join(' · ')}
                  trailing={<PointsBadge points={child.points} />}
                >
                  {child.total > 0 && (
                    <ProgressBar className="mt-2" value={child.completed} max={child.total} label={`${child.name}: ${String(child.completed)} of ${String(child.total)} done`} />
                  )}
                </ListRow>
              ))}
            </List>
          ) : (
            <EmptyState
              variant="surface"
              icon={Users}
              title="No children yet"
              description="Add the children, then give them chores and rewards."
              action={<RouterButtonLink to="/children" variant="primary">Add a child</RouterButtonLink>}
            />
          ))}
      </Section>

      {live.length > 0 && (
        <Section title="Screen time now" description="Each child’s iPad, and the TV time they have left today.">
          <List aria-label="Screen time now">
            {live.map((c) => (
              <ChildScreens key={c.id} child={c} tvSeconds={tvs.data?.children.find((k) => k.childId === c.id)?.tvSecondsRemaining} />
            ))}
          </List>
        </Section>
      )}

      <Section title="TVs" count={tvs.data ? tvList.length : undefined} actions={tvList.length > 0 && <RouterButtonLink to="/tvs">Manage TVs</RouterButtonLink>}>
        {tvs.isError && <Notice tone="error">{errorMessage(tvs.error)}</Notice>}
        {tvs.data &&
          (tvList.length ? (
            <List aria-label="TVs now">
              {tvList.map((tv) => (
                <TvNow key={tv.id} tv={tv} skew={skew} />
              ))}
            </List>
          ) : (
            <EmptyState
              variant="surface"
              icon={Tv}
              title="No TVs yet"
              description="Add a TV to keep it offline until someone is watching."
              action={<RouterButtonLink to="/tvs">Set up TVs</RouterButtonLink>}
            />
          ))}
      </Section>
    </>
  );
}

function ChildScreens({ child, tvSeconds }: { child: Child; tvSeconds: number | undefined }) {
  const query = $api.useQuery('get', '/children/{childId}/screen-time', { params: { path: { childId: child.id } } }, { refetchInterval: 30_000 });
  const now = useServerNow(query.data?.serverTime, query.dataUpdatedAt);
  const s = query.data ? screenState(query.data, now) : undefined;
  const meta = [s ? `${screenMinutes(s.bankedMinutes)} of iPad time banked` : null, tvSeconds === undefined ? null : `TV: ${minutesLabel(tvSeconds)} left`]
    .filter(Boolean)
    .join(' · ');
  return (
    <ListRow
      leading={<ProfileAvatar name={child.name} color={child.color} size="small" aria-hidden="true" />}
      title={child.name}
      meta={query.isError ? errorMessage(query.error) : meta}
      trailing={
        s &&
        (s.runningSeconds > 0 ? (
          <Tag tone="positive">
            iPad on · <Countdown seconds={s.runningSeconds} />
          </Tag>
        ) : (
          <Tag>iPad locked</Tag>
        ))
      }
    />
  );
}

function TvNow({ tv, skew }: { tv: TvState; skew: number }) {
  const now = useTicking(!!tv.session?.endsAt);
  const s = tv.session;
  let tag;
  let meta: string;
  if (tv.prompt) {
    tag = <Tag tone="attention">Asking who’s watching</Tag>;
    meta = 'Offline until you choose, in the inbox';
  } else if (s) {
    tag = (
      <Tag tone="positive">
        {watcherLabel(s)} watching
        {s.endsAt && (
          <>
            {' · '}
            <Countdown seconds={secondsUntil(s.endsAt, skew, now)} />
          </>
        )}
      </Tag>
    );
    meta = s.watcher === 'child' ? 'On their TV time' : 'Parents are watching';
  } else if (tv.open && tv.openClosesAt) {
    tag = <Tag tone="info">Open</Tag>;
    meta = `Open until ${clockLabel(tv.openClosesAt)}${tv.openReason === 'parent' ? '' : ' (open hours)'}`;
  } else {
    tag = <Tag>Offline</Tag>;
    meta = tv.power === 'on' ? 'On, offline' : 'Offline until someone is watching';
  }
  return <ListRow title={tvTitle(tv)} meta={meta} trailing={tag} />;
}
