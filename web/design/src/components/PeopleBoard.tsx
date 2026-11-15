import type { HTMLAttributes } from 'react';

import { cx } from '../cx';

export interface PeopleBoardProps extends HTMLAttributes<HTMLElement> {
  /** Accessible name of the board. */
  label?: string;
}

/**
 * Today's person columns (PersonCards), side by side and each scrolling on its own, so one person's
 * long list never leaves dead space beside another's. Two columns on a mid-width screen when there
 * are four or more people; stacked on a phone.
 */
export function PeopleBoard({ children, className, label = 'Today by person', ...props }: PeopleBoardProps) {
  return (
    <section {...props} className={cx('oc-people-board', className)} aria-label={label}>
      {children}
    </section>
  );
}
