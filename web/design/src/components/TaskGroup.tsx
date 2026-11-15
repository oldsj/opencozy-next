import type { ComponentType, HTMLAttributes, ReactNode } from 'react';
import { Check, Hourglass, type LucideProps } from 'lucide-react';

import { cx } from '../cx';

export interface TaskGroupProps extends Omit<HTMLAttributes<HTMLElement>, 'title'> {
  /** The routine or group name: "Morning", "Chores". An h3 inside a PersonCard. */
  title: ReactNode;
  /** A Lucide icon for the group (a period such as Sunrise, or ClipboardCheck for chores). */
  icon?: ComponentType<LucideProps>;
  /**
   * The group's state beside the title: "5 points", "Waiting for approval". `earned` shows it in
   * the positive lane ("Points earned").
   */
  status?: ReactNode;
  earned?: boolean;
  /**
   * Finished and waiting for an adult's approval: shows "Waiting for approval" (or `status`) in the
   * attention lane. Its rows usually carry `status="pending"` too.
   */
  pending?: boolean;
  /** Every task is done: the group collapses to its title and a "Done" mark. */
  done?: boolean;
  /** A note under the rows, e.g. a parent's reason: "Not approved yet: shoes are on the stairs. Try again." */
  message?: ReactNode;
  /** TaskRows. Not rendered when `done`. */
  children?: ReactNode;
}

/** A routine or the chores inside a person's column: a titled group of task rows. */
export function TaskGroup({ children, className, done = false, earned = false, icon: Icon, message, pending = false, status, title, ...props }: TaskGroupProps) {
  return (
    <section {...props} className={cx('oc-task-group', done && 'oc-task-group--done', pending && !done && 'oc-task-group--pending', className)}>
      <div className="oc-task-group__header">
        {Icon && <Icon aria-hidden="true" size={19} />}
        <h3 className="oc-task-group__title">{title}</h3>
        {done ? (
          <span className="oc-task-group__status oc-task-group__status--earned">
            <Check aria-hidden="true" size={16} /> Done
          </span>
        ) : pending ? (
          <span className="oc-task-group__status oc-task-group__status--pending">
            <Hourglass aria-hidden="true" size={15} /> {status ?? 'Waiting for approval'}
          </span>
        ) : (
          status && <span className={cx('oc-task-group__status', earned && 'oc-task-group__status--earned')}>{status}</span>
        )}
      </div>
      {!done && children && <div className="oc-task-group__rows">{children}</div>}
      {message && (
        <p className="oc-task-group__message" role="status">
          {message}
        </p>
      )}
    </section>
  );
}
