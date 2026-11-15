import type { HTMLAttributes, ReactNode } from 'react';
import { CircleAlert, CircleCheck, Info, TriangleAlert } from 'lucide-react';

import { cx } from '../cx';

export type NoticeTone = 'info' | 'success' | 'attention' | 'error';

export interface NoticeProps extends Omit<HTMLAttributes<HTMLDivElement>, 'title'> {
  tone?: NoticeTone;
  /** Short bold lead, e.g. "Was this change saved?". */
  title?: ReactNode;
  /** Buttons that resolve the notice. */
  actions?: ReactNode;
}

const ICONS = { info: Info, success: CircleCheck, attention: TriangleAlert, error: CircleAlert } as const;

/** An inline message about the page's state. Errors are announced as alerts, the rest politely. */
export function Notice({ actions, children, className, role, title, tone = 'info', ...props }: NoticeProps) {
  const Icon = ICONS[tone];
  return (
    <div
      {...props}
      className={cx('oc-notice', `oc-notice--${tone}`, className)}
      role={role ?? (tone === 'error' ? 'alert' : 'status')}
    >
      <Icon className="oc-notice__icon" aria-hidden="true" size={20} strokeWidth={2} />
      <div className="oc-notice__body">
        {title && <strong className="oc-notice__title">{title}</strong>}
        {children && <div className="oc-notice__text">{children}</div>}
        {actions && <div className="oc-notice__actions">{actions}</div>}
      </div>
    </div>
  );
}
