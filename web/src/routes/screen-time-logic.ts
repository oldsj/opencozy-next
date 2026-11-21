import { useEffect, useState } from 'react';

import type { components } from '../api/schema.gen';

export type ScreenTime = components['schemas']['ScreenTime'];
export type Credit = components['schemas']['Credit'];
export type TimeChoice = components['schemas']['TimeChoice'];

/** The parent's answers to "Ask for more time", in the order shown. */
export const timeChoices: readonly { value: TimeChoice; label: string }[] = [
  { value: '30min', label: '30 min' },
  { value: '1h', label: '1 hour' },
  { value: 'all_day', label: 'All day' },
  { value: 'no', label: 'No' },
];

export interface ScreenState {
  /** Seconds left of the time running now; 0 when the iPad is locked. */
  runningSeconds: number;
  /** Seconds waiting to start after what is running. */
  queuedSeconds: number;
  /** Whole minutes banked, rounded down (what a child can still start). */
  bankedMinutes: number;
}

/** What is running, queued and banked at `now` (ms since the epoch, on the server's clock). */
export function screenState(st: Pick<ScreenTime, 'windows' | 'bankedSeconds' | 'unlockedUntil'>, now: number): ScreenState {
  const until = st.unlockedUntil ? Date.parse(st.unlockedUntil) : 0;
  const runningSeconds = Math.max(0, Math.ceil((until - now) / 1000));
  let queued = 0;
  for (const w of st.windows) {
    const start = Date.parse(w.startsAt);
    const end = Date.parse(w.endsAt);
    // Windows that start after the current run are queued: each counts in full.
    if (start >= Math.max(until, now)) queued += end - start;
  }
  return { runningSeconds, queuedSeconds: Math.round(queued / 1000), bankedMinutes: Math.floor(st.bankedSeconds / 60) };
}

/** "45 min", "1 hr 30 min", "2 hr". */
export function minutesLabel(minutes: number): string {
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  if (h === 0) return `${String(m)} min`;
  return m === 0 ? `${String(h)} hr` : `${String(h)} hr ${String(m)} min`;
}

/** What a banked credit is, in a parent's words. */
export function creditTitle(c: Pick<Credit, 'source' | 'rewardTitle' | 'grantedBy'>): string {
  switch (c.source) {
    case 'redemption':
      return c.rewardTitle ?? 'A reward';
    case 'grant':
      return c.grantedBy ? `From ${c.grantedBy}` : 'From a parent';
    case 'time_request':
      return c.grantedBy ? `${c.grantedBy} said yes` : 'A yes to more time';
  }
}

/** Minutes left on a credit, rounded down. */
export function creditLeft(c: Pick<Credit, 'seconds' | 'usedSeconds'>): number {
  return Math.floor(Math.max(0, c.seconds - c.usedSeconds) / 60);
}

/**
 * What a child can start from the bank, in one tap: 15, 30 and 60 minutes when there is that much,
 * and the whole bank when it isn't one of those (at most 240, the most one spend takes).
 */
export function spendChoices(bankedMinutes: number): number[] {
  const out = [15, 30, 60].filter((m) => m <= bankedMinutes);
  const all = Math.min(bankedMinutes, 240);
  if (all > 0 && !out.includes(all)) out.push(all);
  return out;
}

/** One website name per line (or separated by commas or spaces), blank lines dropped. The server tidies the rest. */
export function domainsFromText(text: string): string[] {
  return text.split(/[\s,]+/).filter(Boolean);
}

/**
 * The server's clock, ticking once a second: `serverTime` was true when the data arrived at
 * `receivedAt` (ms, this device's clock), so a device whose clock is off still counts down right.
 */
export function useServerNow(serverTime: string | undefined, receivedAt: number): number {
  const [tick, setTick] = useState(() => Date.now());
  useEffect(() => {
    const id = window.setInterval(() => { setTick(Date.now()); }, 1000);
    return () => { window.clearInterval(id); };
  }, []);
  if (!serverTime || !receivedAt) return tick;
  return Date.parse(serverTime) + (tick - receivedAt);
}
