import type { ProfileColor } from '../../src';

/** Slices 4 and 5, typed to the shapes agreed with cozy-rewrite (fictional data). */

/** GET /api/children/{id}/history: the points ledger, newest first. */
export type LedgerKind = 'earned' | 'reversed' | 'redeemed' | 'refunded';

export interface LedgerEntry {
  id: string;
  at: string;
  kind: LedgerKind;
  title: string;
  /** Signed: earned and refunded are positive, reversed and redeemed negative. */
  points: number;
  /** Set on an entry that a later reversal undid. */
  reversedBy?: string | undefined;
  /** Set on the reversal itself. */
  reverses?: string | undefined;
  /** The adult who approved, reversed or refunded it. */
  by?: string | undefined;
}

export interface HistoryResponse {
  balance: number;
  entries: LedgerEntry[];
}

export const historyChild: { id: string; name: string; color: ProfileColor } = { id: 'c1', name: 'Abby', color: 'sky' };

export const history: HistoryResponse = {
  balance: 24,
  entries: [
    { id: 'e6', at: 'Today, 10:20 AM', kind: 'reversed', title: 'Water the plants', points: -3, reverses: 'e4', by: 'Sam' },
    { id: 'e5', at: 'Today, 9:41 AM', kind: 'earned', title: 'Tidy the hallway', points: 5, by: 'Sam' },
    { id: 'e4', at: 'Today, 8:05 AM', kind: 'earned', title: 'Water the plants', points: 3, reversedBy: 'e6', by: 'Sam' },
    { id: 'e3', at: 'Yesterday, 6:10 PM', kind: 'refunded', title: 'Late bedtime', points: 30, by: 'Robin' },
    { id: 'e2', at: 'Yesterday, 5:30 PM', kind: 'redeemed', title: 'Late bedtime', points: -30 },
    { id: 'e1', at: 'Yesterday, 7:58 AM', kind: 'earned', title: 'Morning', points: 3, by: 'Robin' },
  ],
};

/** GET /api/rewards. */
export interface Reward {
  id: string;
  title: string;
  cost: number;
  kind: 'item' | 'screen_time';
  minutes?: number | undefined;
  available: boolean;
}

export interface RewardsResponse {
  rewards: Reward[];
}

export const rewards: RewardsResponse = {
  rewards: [
    { id: 'r1', title: 'Choose a game', cost: 4, kind: 'item', available: true },
    { id: 'r2', title: '20 minutes of iPad', cost: 10, kind: 'screen_time', minutes: 20, available: true },
    { id: 'r3', title: 'Movie night pick', cost: 50, kind: 'item', available: true },
    { id: 'r4', title: 'Late bedtime', cost: 30, kind: 'item', available: false },
  ],
};

/** POST /api/children/{id}/redemptions {rewardId}: the result the kiosk shows. */
export interface RedemptionResult {
  id: string;
  status: 'pending' | 'approved' | 'rejected';
}
