import type { HTMLAttributes, MouseEventHandler, ReactNode } from 'react';

import { cx } from '../cx';
import { Button } from './Button';
import { CheckCircle, type CheckCircleProps } from './CheckCircle';

export type TaskRowStatus = 'open' | 'pending' | 'sending' | 'complete' | 'failed';

const STATUS_LABELS: Record<TaskRowStatus, string | null> = {
  open: null,
  pending: 'Waiting for approval',
  sending: 'Saving…',
  complete: 'Completed · confirmed',
  failed: null,
};

export interface TaskRowCheck extends Pick<CheckCircleProps, 'checked' | 'disabled' | 'label' | 'onClick'> {
  status: 'open' | 'sending' | 'complete' | 'failed';
}

export interface TaskRowProps extends Omit<HTMLAttributes<HTMLElement>, 'title'> {
  /** The task, shown first for quick scanning. */
  title: string;
  /** Whose task it is. */
  assignee?: string;
  /** Whole points the task is worth. */
  points?: number;
  /** A date or schedule, e.g. "Today · morning". */
  metadata?: ReactNode;
  /**
   * Honest state. `pending`: done by the child, waiting for an adult's approval (a lasting state,
   * not loading). `sending`: the tick is in flight. `complete`: confirmed. `failed`: not saved.
   */
  status?: TaskRowStatus;
  /** Label for the built-in action button. */
  actionLabel?: string;
  /** Handler for the built-in action button. */
  onAction?: MouseEventHandler<HTMLButtonElement>;
  /** Custom trailing content when the built-in action is not enough. */
  action?: ReactNode;
  /** The check-circle variant for child-facing lists; an alternative to onAction/action. */
  check?: TaskRowCheck;
  /** A Lucide icon placed before the title. */
  icon?: ReactNode;
  /** Friendly recovery copy for a failed check. */
  error?: ReactNode;
  /**
   * A note on its own line under the title, such as a parent's reason for not approving it
   * ("Not approved yet: the shoes are still on the stairs. Try again."). Wraps; up to a few lines.
   */
  message?: ReactNode;
  /** Play the one-shot settle motion, only for a newly confirmed success. */
  celebrate?: boolean;
}

export function TaskRow({
  action,
  actionLabel = 'Complete task',
  assignee,
  check,
  className,
  celebrate = false,
  error,
  icon,
  message,
  metadata,
  onAction,
  points,
  status = 'open',
  title,
  ...props
}: TaskRowProps) {
  const effective = check?.status ?? status;
  const statusLabel = check ? null : STATUS_LABELS[effective];
  const trailing = check ? (
    <CheckCircle {...check} />
  ) : (
    (action ?? (onAction && effective !== 'complete' ? <Button onClick={onAction}>{actionLabel}</Button> : null))
  );

  return (
    <article
      {...props}
      className={cx(
        'oc-task-row',
        `oc-task-row--${effective}`,
        check && 'oc-task-row--check',
        !icon && 'oc-task-row--no-icon',
        !trailing && 'oc-task-row--inline',
        celebrate && 'oc-task-row--celebrate',
        className,
      )}
      aria-busy={effective === 'sending' || undefined}
    >
      {icon && (
        <span className="oc-task-row__icon" aria-hidden="true">
          {icon}
        </span>
      )}
      <div className="oc-task-row__content">
        {assignee && <p className="oc-task-row__assignee">{assignee}</p>}
        <h3 className="oc-task-row__title">{title}</h3>
        {(points !== undefined || metadata) && (
          <p className="oc-task-row__meta">
            {points !== undefined && <span>{points} points</span>}
            {points !== undefined && metadata && <span aria-hidden="true"> · </span>}
            {metadata}
          </p>
        )}
      </div>
      {statusLabel && <span className={cx('oc-task-row__status', `oc-task-row__status--${effective}`)}>{statusLabel}</span>}
      {trailing}
      {message && <p className="oc-task-row__message">{message}</p>}
      {error && <p className="oc-task-row__error">{error}</p>}
    </article>
  );
}
