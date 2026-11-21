import { useEffect, useState } from 'react';

import type { Choice } from '@app/design';

import type { components } from '../api/schema.gen';

export type TvList = components['schemas']['TvList'];
export type TvState = components['schemas']['TvState'];
export type TvChild = components['schemas']['TvChild'];
export type TvSession = components['schemas']['TvSession'];
export type TvPrompt = components['schemas']['TvPrompt'];
export type OpenHours = components['schemas']['OpenHours'];
export type Weekday = components['schemas']['Weekday'];
export type OpenDuration = components['schemas']['OpenTvs']['duration'];

// The TV queries refresh this often; countdowns tick locally in between.
export const TV_REFRESH_MS = 15_000;

/** "Den TV · Living room", or just the name. */
export const tvTitle = (tv: { name: string; room: string | null }) => (tv.room ? `${tv.name} · ${tv.room}` : tv.name);

/** "25 min", "1 hr 5 min", "no time". Whole minutes, rounded down (never more than is left). */
export function minutesLabel(seconds: number): string {
  if (seconds < 60) return seconds > 0 ? 'under 1 min' : 'no time';
  const minutes = Math.floor(seconds / 60);
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  if (!h) return `${String(m)} min`;
  return m ? `${String(h)} hr ${String(m)} min` : `${String(h)} hr`;
}

/** "4:00 PM", in the browser's locale. */
export const clockLabel = (iso: string) => new Date(iso).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });

/** How far the server's clock is ahead of this one, from a response's serverTime. */
export const clockSkew = (serverTime: string | undefined, receivedAt: number) =>
  serverTime ? new Date(serverTime).getTime() - receivedAt : 0;

/** Whole seconds from now (server time) until end; never below zero. */
export function secondsUntil(end: string, skew: number, now: number): number {
  return Math.max(0, Math.floor((new Date(end).getTime() - (now + skew)) / 1000));
}

/** Date.now(), ticking every second while active. */
export function useTicking(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const timer = setInterval(() => { setNow(Date.now()); }, 1000);
    return () => { clearInterval(timer); };
  }, [active]);
  return now;
}

const days: Weekday[] = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'];
/** DayPicker's numbers (0 Sunday; empty, every day) to the API's days (omitted, every day). */
export const toWeekdays = (picked: readonly number[]): Weekday[] | undefined =>
  picked.length === 0 || picked.length === 7 ? undefined : picked.map((d) => days[d] ?? 'sun');
export const fromWeekdays = (w: readonly Weekday[] | undefined): number[] => (w ? w.map((d) => days.indexOf(d)).sort((a, b) => a - b) : []);

/** Open hours from a form: days omitted when every day. */
export function openHours(start: string, end: string, picked: readonly number[]): OpenHours {
  const w = toWeekdays(picked);
  return w ? { start, end, days: w } : { start, end };
}

/** "9:00 PM to 7:00 AM, Fri and Sat" for a TV's open hours. */
export function openHoursLabel(h: OpenHours): string {
  const at = (clock: string) => {
    const [hh = 0, mm = 0] = clock.split(':').map(Number);
    return new Date(2000, 0, 1, hh, mm).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
  };
  const names = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  const on = fromWeekdays(h.days).map((d) => names[d] ?? '');
  const which = !on.length ? 'every day' : on.length === 1 ? (on[0] ?? '') : `${on.slice(0, -1).join(', ')} and ${on[on.length - 1] ?? ''}`;
  return `${at(h.start)} to ${at(h.end)}, ${which}`;
}

/** MACs as typed, one per line or comma: trimmed, blanks dropped. */
export const parseMacs = (text: string) => text.split(/[\s,]+/).map((m) => m.trim()).filter(Boolean);

/** Who is on a TV, for a row title. */
export const watcherLabel = (s: Pick<TvSession, 'watcher' | 'childName'>) => (s.watcher === 'parents' ? 'Parents' : (s.childName ?? 'A child'));

export type WhoChoice = 'parents' | 'not_now' | 'open' | `child:${string}`;

/**
 * The one-tap answers to "Who's watching?" for one TV: Parents, each child (without time, outside
 * their hours or on another TV, disabled and saying why), Open 1 hour, and Not now.
 */
export function whoChoices(tvId: string, kids: readonly TvChild[], options: { open?: boolean; notNow?: boolean } = {}): Choice<WhoChoice>[] {
  const out: Choice<WhoChoice>[] = [{ value: 'parents', label: 'Parents', variant: 'primary' }];
  for (const kid of kids) {
    if (kid.watchingTvId && kid.watchingTvId !== tvId) {
      out.push({ value: `child:${kid.childId}`, label: kid.name, detail: 'on another TV', disabled: true });
      continue;
    }
    const opens = kid.hours && !kid.hours.open && kid.hours.opensAt ? clockLabel(kid.hours.opensAt) : null;
    const detail = kid.tvSecondsRemaining <= 0 ? 'no time' : opens ? `from ${opens}` : minutesLabel(kid.tvSecondsRemaining);
    out.push({ value: `child:${kid.childId}`, label: kid.name, detail, disabled: kid.tvSecondsRemaining <= 0 || opens !== null });
  }
  if (options.open) out.push({ value: 'open', label: 'Open 1 hour' });
  if (options.notNow) out.push({ value: 'not_now', label: 'Not now', variant: 'quiet' });
  return out;
}

const sources: Record<TvPrompt['source'], string> = {
  display: 'Asked from the family display',
  adult: 'Asked by a parent',
  router: 'Trying to go online',
  home_assistant: 'Turned on',
};
export const promptSource = (p: Pick<TvPrompt, 'source'>) => sources[p.source];

const endReasons: Record<NonNullable<TvSession['endReason']>, string> = {
  tv_off: 'TV turned off',
  time_up: 'Time was up',
  parent_ended: 'Turned off by a parent',
  done: 'Said they were done',
  open_hours: 'Open hours began',
  parent_override: 'A parent opened the TV',
};
export const endReasonLabel = (r: TvSession['endReason']) => (r ? endReasons[r] : 'Watching now');
