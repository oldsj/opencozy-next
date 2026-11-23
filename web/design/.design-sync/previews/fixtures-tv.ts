import type { ProfileColor } from '../../src';

/**
 * Slice 9 (the TV gate), shaped like the old API's "TV screen time" (fictional data). TVs stay offline
 * until someone is watching; the router lets a TV online only while it has a session or is open.
 * Times are household time; the builders adjust names to the new contract.
 */
export type TvPower = 'on' | 'off' | 'unknown';
export type Watcher = 'parents' | 'child';
export type TvEndReason = 'tv_off' | 'time_up' | 'parent_ended' | 'done' | 'open_hours' | 'parent_override';

export interface TvSession {
  id: string;
  watcher: Watcher;
  childId: string | null;
  childName: string | null;
  startedAt: string;
  /** Null for parents (only a 4-hour safety cap, never shown as a countdown). */
  endsAt: string | null;
  secondsRemaining: number | null;
  /** The child's allowed hours end it before their balance runs out. */
  endsWithHours?: boolean | undefined;
}

/** A child as offered in "Who's watching?". */
export interface PromptChild {
  childId: string;
  name: string;
  color: ProfileColor;
  tvSecondsRemaining: number;
  /** Has TV time, is inside their hours and isn't watching another TV. */
  canWatch: boolean;
  /** Why not, in words, when canWatch is false. */
  reason?: string | undefined;
}

export interface TvPrompt {
  id: string;
  version: number;
  source: 'home_assistant' | 'router' | 'display';
  raisedAt: string;
  children: PromptChild[];
}

export interface Tv {
  id: string;
  name: string;
  room: string | null;
  /** 1 to 8, each once per household (adults only; never on the display). */
  macs: string[];
  haEntityId: string | null;
  power: TvPower;
  /** One daily window, "HH:MM"; an end before the start is the next morning. */
  openHours: { start: string; end: string } | null;
  /** Online without a session: inside open hours, or opened by a parent until then. */
  openUntil: string | null;
  openReason: 'open_hours' | 'parent' | null;
  session: TvSession | null;
  prompt: TvPrompt | null;
}

export const durationLabels = { '1h': '1 hour', '2h': '2 hours', all_day: 'All day' } as const;

const kidsOffered: PromptChild[] = [
  { childId: 'c1', name: 'Abby', color: 'sky', tvSecondsRemaining: 45 * 60, canWatch: true },
  { childId: 'c2', name: 'Ezra', color: 'sage', tvSecondsRemaining: 0, canWatch: false, reason: 'no TV time' },
  { childId: 'c3', name: 'Noor', color: 'lilac', tvSecondsRemaining: 20 * 60, canWatch: false, reason: 'from 4:00 PM' },
];

export const tvs: Tv[] = [
  {
    id: 't1',
    name: 'Den TV',
    room: 'Den',
    macs: ['02:00:5e:10:00:01', '02:00:5e:10:00:02'],
    haEntityId: 'media_player.den_tv',
    power: 'on',
    openHours: null,
    openUntil: null,
    openReason: null,
    session: null,
    prompt: { id: 'p1', version: 1, source: 'home_assistant', raisedAt: '4:12 PM', children: kidsOffered },
  },
  {
    id: 't2',
    name: 'Living room TV',
    room: 'Living room',
    macs: ['02:00:5e:10:00:03'],
    haEntityId: 'media_player.living_room_tv',
    power: 'on',
    openHours: null,
    openUntil: null,
    openReason: null,
    session: { id: 's1', watcher: 'child', childId: 'c2', childName: 'Ezra', startedAt: '3:50 PM', endsAt: '4:24 PM', secondsRemaining: 4 * 60 + 10 },
    prompt: null,
  },
  {
    id: 't3',
    name: 'Basement TV',
    room: 'Basement',
    macs: ['02:00:5e:10:00:04'],
    haEntityId: null,
    power: 'unknown',
    openHours: null,
    openUntil: null,
    openReason: null,
    session: { id: 's2', watcher: 'parents', childId: null, childName: null, startedAt: '3:30 PM', endsAt: null, secondsRemaining: null },
    prompt: null,
  },
  {
    id: 't4',
    name: 'Bedroom TV',
    room: 'Bedroom',
    macs: ['02:00:5e:10:00:05'],
    haEntityId: 'media_player.bedroom_tv',
    power: 'off',
    openHours: { start: '21:00', end: '07:00' },
    openUntil: null,
    openReason: null,
    session: null,
    prompt: null,
  },
];

/** A child's TV time on /kid (balance spent only while watching). */
export interface KidTv {
  secondsRemaining: number;
  /** TVs asking "Who's watching?" now that this child could start. */
  waiting: { tvId: string; tvName: string }[];
  watching: { tvId: string; tvName: string; secondsRemaining: number; endsWithHours: boolean } | null;
  hoursOpen: boolean;
  opensAt: string | null;
}

export const kidTvStates = {
  idle: { secondsRemaining: 45 * 60, waiting: [], watching: null, hoursOpen: true, opensAt: null },
  waiting: { secondsRemaining: 45 * 60, waiting: [{ tvId: 't1', tvName: 'Den TV' }], watching: null, hoursOpen: true, opensAt: null },
  watching: { secondsRemaining: 21 * 60, waiting: [], watching: { tvId: 't1', tvName: 'Den TV', secondsRemaining: 21 * 60, endsWithHours: false }, hoursOpen: true, opensAt: null },
  endsWithHours: { secondsRemaining: 40 * 60, waiting: [], watching: { tvId: 't1', tvName: 'Den TV', secondsRemaining: 12 * 60, endsWithHours: true }, hoursOpen: true, opensAt: null },
  outsideHours: { secondsRemaining: 45 * 60, waiting: [], watching: null, hoursOpen: false, opensAt: '4:00 PM' },
  none: { secondsRemaining: 0, waiting: [{ tvId: 't1', tvName: 'Den TV' }], watching: null, hoursOpen: true, opensAt: null },
} satisfies Record<string, KidTv>;
