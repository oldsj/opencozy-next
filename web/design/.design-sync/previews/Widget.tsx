// @dsCard group="Dashboard"
import { CalendarDays, DoorOpen, House, Lock, Thermometer, Tv, UtensilsCrossed, type LucideIcon } from 'lucide-react';

import { AppFrame, Button, Reading, ButtonLink, Countdown, EmptyState, List, ListRow, PageHeader, ProfileAvatar, Section, Tag, TopBar, Widget } from '../../src';

import { calendar, dayHeadings, dayOf, kids, todayDate, when, type CalendarEvent } from './fixtures-calendar';

import { entities, readingFor, type HomeEntity } from './fixtures-home';
import { tvs } from './fixtures-tv';

import { mealSlots, slotLabels, today, week, type MealDay } from './fixtures-meals';

/**
 * Today's meals, slot by slot; an empty slot says so instead of disappearing. On a display, render
 * it only when the display has the meals scope; without it, leave the widget out entirely rather
 * than showing a hint (the feature exists, it is just not shared with this screen).
 */
export function MealsToday({ day = week.days.find((d) => d.date === today) }: { day?: MealDay | undefined }) {
  const meals = day?.meals ?? [];
  return (
    <Widget
      title="Meals"
      icon={UtensilsCrossed}
      action={
        <ButtonLink variant="quiet" href="#meals">
          This week
        </ButtonLink>
      }
    >
      {meals.length === 0 ? (
        <EmptyState title="Nothing planned today" description="Plan meals for the week from the adult pages." />
      ) : (
        <List>
          {mealSlots.map((slot) => {
            const meal = meals.find((m) => m.slot === slot);
            return (
              <ListRow
                key={slot}
                muted={meal === undefined}
                title={meal?.title ?? 'Nothing planned'}
                meta={[slotLabels[slot], meal?.note].filter(Boolean).join(' · ')}
              />
            );
          })}
        </List>
      )}
    </Widget>
  );
}

export function MealsEmptyDay() {
  return <MealsToday day={week.days.find((d) => d.date === '2026-09-17')} />;
}

/** The frame on its own, with a footer line. */
export function Frame() {
  return (
    <Widget title="Calendar" icon={CalendarDays} footer="Updated 7:40 AM">
      <p>A widget holds one glanceable block; the full view sits behind its action.</p>
    </Widget>
  );
}

/** One event: when, then what; whose it is as avatars; tentative events quieter. Never a description. */
function EventRow({ event }: { event: CalendarEvent }) {
  const tentative = event.status === 'tentative';
  return (
    <ListRow
      stackOnNarrow
      muted={tentative}
      title={event.title ?? 'Busy'}
      meta={[when(event), event.calendar.name, event.location].filter(Boolean).join(' · ')}
      trailing={
        <>
          {tentative ? <Tag>Maybe</Tag> : null}
          {event.children.map((id) => {
            const kid = kids[id];
            return kid ? <ProfileAvatar key={id} name={kid.name} color={kid.color} size="small" /> : null;
          })}
        </>
      }
    />
  );
}

/** Today's events on the wall display: all-day first, then by time. */
export function CalendarToday() {
  const events = calendar.events.filter((e) => dayOf(e) === todayDate).sort((a, b) => Number(b.allDay) - Number(a.allDay) || a.start.localeCompare(b.start));
  return (
    <Widget
      title="Today"
      icon={CalendarDays}
      action={
        <ButtonLink variant="quiet" href="#agenda">
          Agenda
        </ButtonLink>
      }
      footer="Updated 7:40 AM"
    >
      {events.length === 0 ? (
        <EmptyState title="Nothing on the calendar" description="Enjoy the quiet day." />
      ) : (
        <List>
          {events.map((event) => (
            <EventRow key={event.id} event={event} />
          ))}
        </List>
      )}
    </Widget>
  );
}

/** The family agenda (view with ?surface=kiosk): the next days, one section each. */
export function Agenda() {
  const days = [...new Set(calendar.events.map(dayOf))].sort();
  return (
    <AppFrame topBar={<TopBar time="7:42" date="Monday, September 14" householdName="Maple House" />}>
      <PageHeader title="Agenda" description="The family calendars, the next few days." />
      {days.map((day) => (
        <Section key={day} title={dayHeadings[day] ?? day}>
          <List>
            {calendar.events
              .filter((e) => dayOf(e) === day)
              .map((event) => (
                <EventRow key={event.id} event={event} />
              ))}
          </List>
        </Section>
      ))}
    </AppFrame>
  );
}

/** Which screens are on, who's watching and the time left (the wall display and the adult dashboard). */
export function WatchingNow() {
  return (
    <Widget title="Watching now" icon={Tv}>
      <List>
        <ListRow
          leading={<ProfileAvatar name="Abby" color="sky" size="small" aria-hidden="true" />}
          title="Living room TV"
          meta="Abby"
          trailing={
            <Tag tone="attention">
              <Countdown seconds={240} />
            </Tag>
          }
        />
        <ListRow
          leading={<ProfileAvatar name="Ezra" color="sage" size="small" aria-hidden="true" />}
          title="Ezra’s iPad"
          meta="Ezra"
          trailing={
            <Tag tone="info">
              <Countdown seconds={1499} />
            </Tag>
          }
        />
        <ListRow title="Basement TV" meta="Parents" trailing={<Tag>No limit</Tag>} />
        <ListRow muted title="Bedroom TV" meta="Off" />
      </List>
    </Widget>
  );
}

/**
 * The display's TVs card: who is watching and how long is left. An off TV offers "Turn on…" (asks a
 * parent; the display never starts a session itself); a running session offers "Done watching".
 */
export function TvsOnDisplay() {
  return (
    <Widget title="TVs" icon={Tv}>
      <List>
        {tvs.map((tv) => {
          const s = tv.session;
          if (s) {
            return (
              <ListRow
                key={tv.id}
                title={tv.name}
                meta={s.watcher === 'parents' ? 'Parents' : (s.childName ?? 'A child')}
                trailing={
                  <>
                    {s.secondsRemaining !== null && (
                      <Tag tone="attention">
                        <Countdown seconds={s.secondsRemaining} />
                      </Tag>
                    )}
                    <Button aria-label={`Done watching ${tv.name}`}>Done watching</Button>
                  </>
                }
              />
            );
          }
          if (tv.prompt) {
            return <ListRow key={tv.id} title={tv.name} meta="Asking a parent…" trailing={<Tag tone="attention">Waiting</Tag>} />;
          }
          if (tv.openUntil !== null) return <ListRow key={tv.id} title={tv.name} meta={`Open until ${tv.openUntil}`} />;
          return (
            <ListRow
              key={tv.id}
              muted
              title={tv.name}
              meta="Off"
              trailing={<Button aria-label={`Turn on ${tv.name}`}>Turn on…</Button>}
            />
          );
        })}
      </List>
    </Widget>
  );
}

function homeIcon(e: HomeEntity): LucideIcon | undefined {
  if (e.entityId.startsWith('lock.')) return Lock;
  if (e.status === 'ok' || e.status === 'stale') {
    if (e.attributes.device_class === 'temperature') return Thermometer;
    if (e.attributes.device_class === 'door') return DoorOpen;
  }
  return undefined;
}

/**
 * Home (slice 15): readings an adult shares from Home Assistant. Read-only; the display never
 * controls anything. Each reading keeps its own freshness, so one failing entity never blanks the
 * others. On a display, leave the widget out entirely while Home Assistant isn't connected.
 */
export function HomeNow({ shown = entities, title = 'Home' }: { shown?: HomeEntity[]; title?: string }) {
  return (
    <Widget title={title} icon={House}>
      <div className="oc-readings">
        {shown.map((e) => (
          <Reading key={e.entityId} icon={homeIcon(e)} {...readingFor(e)} />
        ))}
      </div>
    </Widget>
  );
}

/** A small Home widget with only the entities the adult picked for this spot, under their own title. */
export function HomeDoors({ title = 'Home' }: { title?: string }) {
  return <HomeNow title={title} shown={entities.filter((e) => e.entityId === 'binary_sensor.back_door' || e.entityId === 'lock.front_door')} />;
}
