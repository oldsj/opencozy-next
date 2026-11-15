import type { ReactNode } from 'react';

import { Notice, type NoticeProps } from './Notice';

export type ConnectionBannerStatus = 'live' | 'stale' | 'offline';

export interface ConnectionBannerProps extends Omit<NoticeProps, 'tone' | 'title' | 'children'> {
  /** `live` renders nothing. */
  status: ConnectionBannerStatus;
  /** When the shown data was last confirmed, already formatted for the household: "4:12 pm". */
  lastUpdated?: ReactNode;
  /** Overrides the default sentence. */
  children?: ReactNode;
}

const TITLES = { stale: 'This may be out of date', offline: 'Offline — changes paused' } as const;
const BODIES = {
  stale: 'OpenCozy could not be reached, so this shows the last information it had.',
  offline: 'Ticks and requests wait until the connection is back.',
} as const;

/**
 * Says the screen is showing saved data (`stale`) or cannot send anything (`offline`), and when it
 * was last updated. It sits above the content it describes; the TopBar's StatusPill only flags it.
 */
export function ConnectionBanner({ children, lastUpdated, status, ...props }: ConnectionBannerProps) {
  if (status === 'live') return null;
  return (
    <Notice {...props} tone="attention" title={TITLES[status]}>
      {children ?? BODIES[status]}
      {lastUpdated != null && <> Last updated {lastUpdated}.</>}
    </Notice>
  );
}
