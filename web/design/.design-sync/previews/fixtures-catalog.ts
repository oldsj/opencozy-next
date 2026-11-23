import type { ProfileColor } from '../../src';

/**
 * Editing what's on offer (gaps in slices 2-5), typed to api/src/chores.yaml and rewards.yaml, plus
 * what editing needs that the contract doesn't have yet: a `version` for PUT with expectedVersion,
 * and the redemption list an adult works through. Fictional data.
 */

/** 0 is Sunday. An empty list means every day. */
export type Days = number[];

export const dayShort = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'] as const;
export const dayLong = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'] as const;

export function daysLabel(days: Days): string {
  if (days.length === 0 || days.length === 7) return 'Every day';
  const sorted = [...days].sort((a, b) => a - b);
  if (sorted.join() === '1,2,3,4,5') return 'Weekdays';
  if (sorted.join() === '0,6') return 'Weekends';
  return sorted.map((d) => dayShort[d] ?? '').join(', ');
}

export interface Kid {
  id: string;
  name: string;
  color: ProfileColor;
}

export const kids: Kid[] = [
  { id: 'c1', name: 'Abby', color: 'sky' },
  { id: 'c2', name: 'Ezra', color: 'sage' },
  { id: 'c3', name: 'Noor', color: 'lilac' },
];

/** Chore, with the version an edit sends back (PUT /chores/{id} {expectedVersion, childId, title, points, days}). */
export interface Chore {
  id: string;
  childId: string;
  title: string;
  points: number;
  days: Days;
  version: number;
}

export interface Routine {
  id: string;
  childId: string;
  title: string;
  points: number;
  days: Days;
  /** 1 to 20, in order. An edit keeps the ids of steps it keeps. */
  steps: { id: string; title: string }[];
  version: number;
}

export interface Reward {
  id: string;
  title: string;
  cost: number;
  kind: 'item' | 'screen_time';
  minutes?: number | undefined;
  available: boolean;
  version: number;
}

export const chores: Chore[] = [
  { id: 'ch1', childId: 'c2', title: 'Feed the cat', points: 4, days: [], version: 3 },
  { id: 'ch2', childId: 'c1', title: 'Tidy the hallway', points: 5, days: [1, 3, 5], version: 1 },
  { id: 'ch3', childId: 'c3', title: 'Water the plants', points: 3, days: [0, 6], version: 2 },
];

export const routines: Routine[] = [
  {
    id: 'ro1',
    childId: 'c1',
    title: 'Morning',
    points: 5,
    days: [1, 2, 3, 4, 5],
    steps: [
      { id: 's1', title: 'Get dressed' },
      { id: 's2', title: 'Brush teeth' },
      { id: 's3', title: 'Pack school bag' },
    ],
    version: 4,
  },
  { id: 'ro2', childId: 'c2', title: 'Bedtime', points: 3, days: [], steps: [{ id: 's4', title: 'Pyjamas' }, { id: 's5', title: 'Brush teeth' }], version: 1 },
];

export const catalogRewards: Reward[] = [
  { id: 'rw1', title: 'Movie night pick', cost: 30, kind: 'item', available: true, version: 2 },
  { id: 'rw2', title: '30 min of iPad', cost: 20, kind: 'screen_time', minutes: 30, available: true, version: 1 },
  { id: 'rw3', title: 'Stay up 15 minutes', cost: 25, kind: 'item', available: false, version: 3 },
];

/**
 * A redemption as an adult sees it. `approved` is waiting to be given; `fulfilled` is done (screen
 * time is fulfilled at once). Cancelling refunds the points, or what's left unused of screen time.
 */
export interface RedemptionRow {
  id: string;
  child: Kid;
  title: string;
  cost: number;
  status: 'pending' | 'approved' | 'fulfilled' | 'rejected' | 'cancelled';
  requestedAt: string;
  fulfilledAt?: string | undefined;
  /** Screen time: minutes bought and used so far. */
  minutes?: number | undefined;
  usedMinutes?: number | undefined;
  refundedPoints?: number | undefined;
  version: number;
}

export const redemptions: RedemptionRow[] = [
  { id: 'rd1', child: { id: 'c1', name: 'Abby', color: 'sky' }, title: 'Movie night pick', cost: 30, status: 'approved', requestedAt: 'Today 4:02 PM', version: 2 },
  { id: 'rd2', child: { id: 'c3', name: 'Noor', color: 'lilac' }, title: '30 min of iPad', cost: 20, status: 'fulfilled', requestedAt: 'Today 3:15 PM', fulfilledAt: 'Today 3:15 PM', minutes: 30, usedMinutes: 18, version: 1 },
  { id: 'rd3', child: { id: 'c2', name: 'Ezra', color: 'sage' }, title: 'Stay up 15 minutes', cost: 25, status: 'fulfilled', requestedAt: 'Yesterday', fulfilledAt: 'Yesterday', version: 3 },
  { id: 'rd4', child: { id: 'c2', name: 'Ezra', color: 'sage' }, title: 'Movie night pick', cost: 30, status: 'cancelled', requestedAt: 'Sep 12', refundedPoints: 30, version: 3 },
];

/** Points back when cancelling: the full cost, or for screen time, the unused share rounded down. */
export function refundFor(r: RedemptionRow): number {
  if (r.minutes === undefined || r.usedMinutes === undefined) return r.cost;
  return Math.floor((r.cost * (r.minutes - r.usedMinutes)) / r.minutes);
}

export interface HouseholdSettings {
  name: string;
  /** IANA zone; the household day follows it. */
  timeZone: string;
  version: number;
}

export const household: HouseholdSettings = { name: 'The Rivera household', timeZone: 'America/New_York', version: 2 };

export const timeZones = ['America/New_York', 'America/Chicago', 'America/Denver', 'America/Los_Angeles', 'Europe/London', 'Europe/Amsterdam', 'Asia/Tokyo', 'Australia/Sydney'];

export const catalogErrorText = {
  version_conflict: 'Someone else changed this while you were editing. It now shows their version; check it and save again.',
  already_used: 'That iPad time has already been used, so there’s nothing to refund.',
  no_such_child: 'That child isn’t in the household any more. Choose someone else.',
  no_such_step: 'A step was removed while you were editing. Check the routine and save again.',
  minutes_mismatch: 'Give minutes for iPad time, and none for other rewards.',
  not_approved: 'Only an approved reward can be marked as given.',
  not_cancellable: 'This reward has already been given or cancelled.',
  time_zone_unknown: 'Choose a time zone from the list.',
} as const;
