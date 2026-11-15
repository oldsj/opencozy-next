import { devices } from './fixtures-devices';

/**
 * Slice 8 (iPad time), shaped like the old API (docs/api.md: Screen-time rewards, Ask for more
 * time, Allowed hours, Lock screen message, Blocked websites). Fictional data; the builders adjust
 * names to the new contract. Times are household time.
 */

/** "HH:MM", 24-hour. */
export type Clock = string;

/** A child's allowed hours: school nights are Sunday to Thursday. Null means no rule. */
export interface AllowedHours {
  schoolNight: { start: Clock; end: Clock };
  weekend: { start: Clock; end: Clock };
}

/** Where today stands against the allowed hours. */
export interface HoursNow {
  open: boolean;
  /** Today's window end while open. */
  closesAt: string | null;
  /** The next window's start while closed. */
  opensAt: string | null;
}

export type TimeRequestChoice = '30min' | '1h' | 'all_day' | 'no';

export interface TimeRequest {
  id: string;
  state: 'pending' | 'answered';
  choice: TimeRequestChoice | null;
  requestedAt: string;
  decidedAt: string | null;
  /** When a yes ends. */
  grantedUntil: string | null;
}

/** GET the child's iPad time (kid view): banked time, the open window, hours and the latest ask. */
export interface PersonalTime {
  childId: string;
  /** Banked, not yet spent. */
  secondsRemaining: number;
  /** End of the window covering now, or null while locked. */
  unlockedUntil: string | null;
  /** Seconds left in the current window (derived from unlockedUntil and serverTime). */
  secondsLeftNow: number;
  allowedHours: HoursNow | null;
  /** The latest request, while pending and for an hour after the answer. */
  timeRequest: TimeRequest | null;
}

/** The kid view spends 30 minutes at a time (1-240 allowed); clipped at the end of allowed hours. */
export const SPEND_MINUTES = 30;

export const choiceLabels: Record<TimeRequestChoice, string> = { '30min': '30 min', '1h': '1 hour', all_day: 'All day', no: 'No' };

/** Blocked websites: portals always; games except while the child's window is open. Up to 500 each. */
export interface WebFilter {
  gamePortals: string[];
  games: string[];
}

export const LOCK_MESSAGE_MAX = 200;
/** The lock message comes with the devices list (fixtures-devices.ts). */
export const DEFAULT_LOCK_MESSAGE = devices.defaultLockMessage;

const base: PersonalTime = {
  childId: 'c1',
  secondsRemaining: 45 * 60,
  unlockedUntil: null,
  secondsLeftNow: 0,
  allowedHours: { open: true, closesAt: '7:30 PM', opensAt: null },
  timeRequest: null,
};

/** The kid view's states. */
export const timeStates = {
  locked: base,
  unlocked: { ...base, secondsRemaining: 15 * 60, unlockedUntil: '4:42 PM', secondsLeftNow: 24 * 60 + 13 },
  outsideHours: { ...base, allowedHours: { open: false, closesAt: null, opensAt: '8:00 AM tomorrow' } },
  nothingBanked: { ...base, secondsRemaining: 0 },
  asked: { ...base, secondsRemaining: 0, timeRequest: { id: 't1', state: 'pending', choice: null, requestedAt: '4:05 PM', decidedAt: null, grantedUntil: null } },
  yes: {
    ...base,
    secondsRemaining: 0,
    unlockedUntil: '4:40 PM',
    secondsLeftNow: 29 * 60 + 40,
    timeRequest: { id: 't1', state: 'answered', choice: '30min', requestedAt: '4:05 PM', decidedAt: '4:10 PM', grantedUntil: '4:40 PM' },
  },
  no: { ...base, secondsRemaining: 0, timeRequest: { id: 't1', state: 'answered', choice: 'no', requestedAt: '4:05 PM', decidedAt: '4:10 PM', grantedUntil: null } },
} satisfies Record<string, PersonalTime>;

export const adaHours: AllowedHours = { schoolNight: { start: '15:30', end: '19:30' }, weekend: { start: '08:00', end: '20:00' } };

export const webFilter: WebFilter = {
  gamePortals: ['games.example.test', 'arcade.example.test'],
  games: ['blocks.example.test', 'racer.example.test', 'puzzle.example.test'],
};
