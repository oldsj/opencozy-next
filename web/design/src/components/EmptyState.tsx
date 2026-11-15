import type { ComponentType, HTMLAttributes, ReactNode } from 'react';
import type { LucideProps } from 'lucide-react';

import { cx } from '../cx';

export interface EmptyStateProps extends Omit<HTMLAttributes<HTMLDivElement>, 'title'> {
  title: ReactNode;
  description?: ReactNode;
  /** A Lucide icon that fits the message (a check only for "all done"). */
  icon?: ComponentType<LucideProps>;
  /** One way forward, e.g. an "Add reward" button. */
  action?: ReactNode;
  /** `plain` sits on any surface; `surface` stands in for an empty list with the same quiet frame. */
  variant?: 'plain' | 'surface';
}

export function EmptyState({ action, className, description, icon: Icon, title, variant = 'plain', ...props }: EmptyStateProps) {
  return (
    <div {...props} className={cx('oc-empty-state', `oc-empty-state--${variant}`, className)}>
      {Icon && <Icon aria-hidden="true" size={28} strokeWidth={1.8} />}
      <strong>{title}</strong>
      {description && <p>{description}</p>}
      {action && <div className="oc-empty-state__action">{action}</div>}
    </div>
  );
}
