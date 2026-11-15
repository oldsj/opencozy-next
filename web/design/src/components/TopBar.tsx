import type { HTMLAttributes, ReactNode } from 'react';

import { cx } from '../cx';

export interface TopBarProps extends HTMLAttributes<HTMLElement> {
  /** Local time formatted for the household, e.g. "10:53". */
  time: ReactNode;
  /** Day period such as "PM", set smaller beside the time. Omit for 24-hour clocks. */
  period?: ReactNode;
  /** Friendly local date, e.g. "Sunday, September 13". */
  date: ReactNode;
  householdName?: ReactNode;
  /** Degraded connection status (a StatusPill). The healthy state renders nothing. */
  status?: ReactNode;
  /** Weather or adult-management controls. */
  actions?: ReactNode;
}

/** The ambient clock and date at the top of family screens. */
export function TopBar({ actions, className, date, householdName, period, status, time, ...props }: TopBarProps) {
  return (
    <header {...props} className={cx('oc-top-bar', className)}>
      <div className="oc-top-bar__ambient">
        <p className="oc-top-bar__time">
          <time>{time}</time>
          {period && <span className="oc-top-bar__period">{period}</span>}
        </p>
        <div className="oc-top-bar__context">
          <p className="oc-top-bar__date">{date}</p>
          {householdName && <p className="oc-top-bar__household">{householdName}</p>}
        </div>
      </div>
      {(status ?? actions) && (
        <div className="oc-top-bar__actions">
          {status}
          {actions}
        </div>
      )}
    </header>
  );
}
