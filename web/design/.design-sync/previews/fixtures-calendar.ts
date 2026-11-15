import type { ProfileColor } from '../../src';

/**
 * Slice 13, typed to the shapes agreed with opencozy-pilot (fictional data). Household days;
 * timed events are RFC3339, all-day events are dates with an exclusive end. Cancelled events are
 * never sent.
 */
export interface CalendarRef {
  id: string;
  name: string;
  /** Picked by an adult; a remote hex is never sent. */
  color: ProfileColor;
}

export interface CalendarEvent {
  id: string;
  seriesId: string;
  /** Up to 500 characters, rarely missing: show "Busy". Design for 80 visible. */
  title?: string | undefined;
  allDay: boolean;
  start: string;
  end: string;
  status: 'confirmed' | 'tentative';
  calendar: CalendarRef;
  /** Up to 500 characters. */
  location?: string | undefined;
  /** Which children it is for. */
  children: string[];
}

/** GET /api/calendar?from&to (at most 42 days). */
export interface CalendarResponse {
  events: CalendarEvent[];
}

/** GET /api/calendar/events/{id}, adults only. Displays never get descriptions. */
export interface CalendarEventDetail extends CalendarEvent {
  /** Up to 4000 characters. */
  description?: string | undefined;
  recurring: boolean;
}

/** PUT /api/calendar/events/{id}/children. */
export interface AssignChildrenRequest {
  children: string[];
  scope: 'series' | 'occurrence';
}

export type CalendarErrorCode =
  | 'timeout'
  | 'not_found'
  | 'access_denied'
  | 'too_large'
  | 'invalid_calendar'
  | 'too_many_events'
  | 'rate_limited'
  | 'token_expired';

/** GET /api/calendars (adult settings). The feed URL is never shown back. */
export interface CalendarSource {
  id: string;
  /** Up to 80 characters. */
  name: string;
  provider: 'ics' | 'google' | 'icloud';
  color: ProfileColor;
  showOnDisplay: boolean;
  status: 'ok' | 'error' | 'needs_reauth';
  lastSyncAt?: string | undefined;
  error?: CalendarErrorCode | undefined;
  /** For PATCH and DELETE (the conflict check); never shown. */
  version: number;
}

/** One sentence per sync error, safe to show adults. */
export const calendarErrorText: Record<CalendarErrorCode, string> = {
  timeout: 'The calendar took too long to answer. OpenCozy will try again soon.',
  not_found: 'The calendar address no longer exists. Check the link, or remove it.',
  access_denied: 'The calendar refused access. It may have been made private.',
  too_large: 'The calendar is too big to load. Try a calendar with fewer past events.',
  invalid_calendar: 'That address isn’t a calendar OpenCozy can read.',
  too_many_events: 'The calendar has more events than OpenCozy can show. Older ones were left out.',
  rate_limited: 'The provider asked OpenCozy to slow down. It will try again later.',
  token_expired: 'The sign-in has expired. Sign in again to keep it up to date.',
};

export const providerLabels: Record<CalendarSource['provider'], string> = { ics: 'Calendar link', google: 'Google', icloud: 'iCloud' };

export const kids: Record<string, { name: string; color: ProfileColor }> = {
  c1: { name: 'Ada', color: 'sky' },
  c2: { name: 'Ezra', color: 'sage' },
  c3: { name: 'Noor', color: 'lilac' },
};

const family: CalendarRef = { id: 'k1', name: 'Family', color: 'teal' };
const school: CalendarRef = { id: 'k2', name: 'School', color: 'butter' };
const sports: CalendarRef = { id: 'k3', name: 'Sports', color: 'peach' };

export const todayDate = '2026-09-14';

const swim: CalendarEvent = { id: 'v3', seriesId: 's3', allDay: false, start: '2026-09-14T15:30:00-04:00', end: '2026-09-14T16:30:00-04:00', status: 'confirmed', calendar: sports, title: 'Swimming lesson', location: 'Riverside Pool, lane 3', children: ['c1'] };

export const calendar: CalendarResponse = {
  events: [
    { id: 'v1', seriesId: 's1', allDay: true, start: '2026-09-14', end: '2026-09-17', status: 'confirmed', calendar: family, title: 'Grandma visiting', children: [] },
    { id: 'v2', seriesId: 's2', allDay: false, start: '2026-09-14T08:15:00-04:00', end: '2026-09-14T08:45:00-04:00', status: 'confirmed', calendar: school, title: 'School drop-off', children: ['c1', 'c2'] },
    swim,
    { id: 'v4', seriesId: 's4', allDay: false, start: '2026-09-14T17:00:00-04:00', end: '2026-09-14T17:30:00-04:00', status: 'tentative', calendar: family, title: 'Call with the plumber', children: [] },
    { id: 'v5', seriesId: 's5', allDay: false, start: '2026-09-14T18:30:00-04:00', end: '2026-09-14T19:30:00-04:00', status: 'confirmed', calendar: family, children: [] },
    { id: 'v6', seriesId: 's6', allDay: false, start: '2026-09-15T09:00:00-04:00', end: '2026-09-15T12:00:00-04:00', status: 'confirmed', calendar: school, title: 'Class trip to the science museum: packed lunch, water bottle and a jacket please', location: 'Meet at the front gate', children: ['c2'] },
    { id: 'v7', seriesId: 's3', allDay: false, start: '2026-09-16T15:30:00-04:00', end: '2026-09-16T16:30:00-04:00', status: 'confirmed', calendar: sports, title: 'Swimming lesson', location: 'Riverside Pool, lane 3', children: ['c1'] },
  ],
};

export const eventDetail: CalendarEventDetail = {
  ...swim,
  recurring: true,
  description: 'Bring goggles and a towel. Pickup is from the side entrance.\nThe last lesson of the term is a family watch day.',
};

export const calendars: CalendarSource[] = [
  { id: 'k1', name: 'Family', provider: 'google', color: 'teal', showOnDisplay: true, status: 'ok', lastSyncAt: '7:40 AM', version: 1 },
  { id: 'k2', name: 'School', provider: 'ics', color: 'butter', showOnDisplay: true, status: 'error', lastSyncAt: 'Yesterday, 6:00 PM', error: 'timeout', version: 1 },
  { id: 'k3', name: 'Sports', provider: 'icloud', color: 'peach', showOnDisplay: true, status: 'needs_reauth', lastSyncAt: 'Sep 10', error: 'token_expired', version: 1 },
  { id: 'k4', name: 'Work', provider: 'google', color: 'sand', showOnDisplay: false, status: 'ok', lastSyncAt: '7:40 AM', version: 1 },
];

/** "3:30 PM" from an RFC3339 time already in household time. */
export function clock(iso: string): string {
  const [h = 0, m = 0] = iso.slice(11, 16).split(':').map(Number);
  const hour = h % 12 === 0 ? 12 : h % 12;
  return `${String(hour)}:${String(m).padStart(2, '0')} ${h < 12 ? 'AM' : 'PM'}`;
}

const shortDays: Record<string, string> = { '2026-09-15': 'Tue', '2026-09-16': 'Wed', '2026-09-17': 'Thu' };

/** When an event happens, as one short line. All-day ends are exclusive. */
export function when(event: CalendarEvent): string {
  if (!event.allDay) return `${clock(event.start)} – ${clock(event.end)}`;
  const last = shortDays[event.end] === undefined ? undefined : Object.keys(shortDays)[Object.keys(shortDays).indexOf(event.end) - 1];
  return last === undefined ? 'All day' : `All day · until ${shortDays[last] ?? last}`;
}

export const dayHeadings: Record<string, string> = { '2026-09-14': 'Today', '2026-09-15': 'Tomorrow', '2026-09-16': 'Wednesday, Sep 16' };

/** The household day an event belongs on (its start). */
export function dayOf(event: CalendarEvent): string {
  return event.start.slice(0, 10);
}
