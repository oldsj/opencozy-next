import type { ProfileColor } from '../../src';

/**
 * The kiosk Today screen's data, one entry per child (fictional). The shape mirrors what the
 * Today API is expected to return; `sending` and `celebrate` are client-only states.
 */
export type TodayItemState = 'open' | 'pending' | 'sending' | 'complete' | 'failed';

export interface TodayItem {
  id: string;
  title: string;
  points?: number | undefined;
  state: TodayItemState;
  /** An adult's reason for sending it back, up to 200 characters. */
  rejectedReason?: string | undefined;
  /** Just confirmed: the tick pops once. */
  celebrate?: boolean | undefined;
}

export interface TodayGroup {
  id: string;
  kind: 'routine' | 'chores';
  title: string;
  status?: string | undefined;
  done: boolean;
  /** Finished, waiting for an adult to approve. */
  pending: boolean;
  items: TodayItem[];
}

export interface TodayChild {
  child: { id: string; name: string; color: ProfileColor; points: number; earnedToday: number; completed: number; total: number };
  groups: TodayGroup[];
}

/** GET /api/today (agreed with cozy-rewrite); connection state is derived on the client. */
export interface TodayResponse {
  children: TodayChild[];
  householdDay: string;
  serverTime: string;
}

export const today: TodayResponse = {
  householdDay: '2026-09-14',
  serverTime: '2026-09-14T07:42:00-04:00',
  children: [
  {
    child: { id: 'c1', name: 'Ada', color: 'sky', points: 24, earnedToday: 6, completed: 3, total: 5 },
    groups: [
      {
        id: 'g1',
        kind: 'routine',
        title: 'Morning',
        status: '3 points',
        done: true,
        pending: false,
        items: [
          { id: 'i1', title: 'Make the bed', state: 'complete' },
          { id: 'i2', title: 'Brush teeth', state: 'complete', celebrate: true },
        ],
      },
      {
        id: 'g2',
        kind: 'chores',
        title: 'Chores',
        done: false,
        pending: false,
        items: [
          { id: 'i3', title: 'Water the plants', points: 3, state: 'complete' },
          { id: 'i4', title: 'Fold the towels', points: 4, state: 'sending' },
          { id: 'i5', title: 'Tidy the reading corner', points: 2, state: 'open' },
        ],
      },
    ],
  },
  {
    child: { id: 'c2', name: 'Ezra', color: 'sage', points: 18, earnedToday: 0, completed: 1, total: 4 },
    groups: [
      {
        id: 'g3',
        kind: 'routine',
        title: 'Morning',
        status: '3 points',
        done: false,
        pending: true,
        items: [
          { id: 'i6', title: 'Get dressed', state: 'pending' },
          { id: 'i7', title: 'Pack school bag', state: 'pending' },
        ],
      },
      {
        id: 'g4',
        kind: 'chores',
        title: 'Chores',
        done: false,
        pending: false,
        items: [
          {
            id: 'i8',
            title: 'Feed the cat',
            points: 4,
            state: 'open',
            rejectedReason: 'The bowl still needs water too. Fill it to the line, then tick it again.',
          },
          { id: 'i9', title: 'Sort the recycling', points: 3, state: 'failed' },
        ],
      },
    ],
  },
  {
    child: { id: 'c3', name: 'Noor', color: 'lilac', points: 6, earnedToday: 0, completed: 0, total: 0 },
    groups: [],
  },
],
};
