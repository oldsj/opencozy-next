import type { ProfileColor } from '../../src';

/** GET /api/inbox, proposed to cozy-rewrite (fictional data). Oldest first. */
export type InboxKind = 'completion' | 'routine' | 'redemption' | 'time_request';

export interface InboxItem {
  id: string;
  kind: InboxKind;
  child: { id: string; name: string; color: ProfileColor };
  title: string;
  detail?: string | undefined;
  points?: number | undefined;
  submittedAt: string;
  minutesRequested?: number | undefined;
}

export interface InboxResponse {
  items: InboxItem[];
}

/** A rejection reason is shown to the child; the API caps it at 200 characters. */
export const REJECT_REASON_MAX = 200;

export const inbox: InboxResponse = {
  items: [
    { id: 'n1', kind: 'routine', child: { id: 'c2', name: 'Ezra', color: 'sage' }, title: 'Morning', points: 3, submittedAt: '7:55 AM' },
    { id: 'n2', kind: 'completion', child: { id: 'c1', name: 'Ada', color: 'sky' }, title: 'Tidy the hallway', points: 5, submittedAt: '9:40 AM' },
    { id: 'n3', kind: 'redemption', child: { id: 'c1', name: 'Ada', color: 'sky' }, title: 'Movie night pick', points: 50, submittedAt: '10:02 AM' },
    { id: 'n4', kind: 'time_request', child: { id: 'c3', name: 'Noor', color: 'lilac' }, title: 'More iPad time', minutesRequested: 30, submittedAt: '10:15 AM' },
  ],
};

export const kindLabels: Record<InboxKind, string> = {
  completion: 'Chore',
  routine: 'Routine',
  redemption: 'Reward',
  time_request: 'Time request',
};
