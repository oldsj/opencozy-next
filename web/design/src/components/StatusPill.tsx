import type { HTMLAttributes } from 'react';
import { LoaderCircle, WifiOff } from 'lucide-react';

import { cx } from '../cx';

export type ConnectionStatus = 'live' | 'reconnecting' | 'offline';

export interface StatusPillProps extends HTMLAttributes<HTMLSpanElement> {
  status: ConnectionStatus;
  label?: string;
}

/** The live-update connection, shown only when it is degraded: `live` renders nothing. */
export function StatusPill({ className, label, status, ...props }: StatusPillProps) {
  if (status === 'live') return null;
  const Icon = status === 'offline' ? WifiOff : LoaderCircle;
  const fallback = status === 'offline' ? 'Offline — changes paused' : 'Reconnecting…';
  return (
    <span {...props} className={cx('oc-status-pill', `oc-status-pill--${status}`, className)} role="status" aria-live="polite">
      <Icon className={status === 'reconnecting' ? 'oc-status-pill__spinner' : undefined} aria-hidden="true" size={18} />
      <span>{label ?? fallback}</span>
    </span>
  );
}
