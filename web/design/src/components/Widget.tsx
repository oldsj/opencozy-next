import type { ComponentType, HTMLAttributes, ReactNode } from 'react';
import type { LucideProps } from 'lucide-react';

import { cx } from '../cx';

export interface WidgetProps extends Omit<HTMLAttributes<HTMLElement>, 'title'> {
  /** The widget's name, an h2 ("Meals", "Calendar"). */
  title: ReactNode;
  /** A Lucide icon beside the title. */
  icon?: ComponentType<LucideProps> | undefined;
  /** One quiet action in the header, such as a "Week" link. */
  action?: ReactNode;
  /** A line under the body, such as when it was last updated. */
  footer?: ReactNode;
}

/**
 * One dashboard widget: a titled surface for a glanceable block (today's meals, the next events).
 * Widgets sit in a grid beside the PeopleBoard on the wall display; keep the body short and put
 * the full view behind the action.
 */
export function Widget({ action, children, className, footer, icon: Icon, title, ...props }: WidgetProps) {
  return (
    <section {...props} className={cx('oc-widget', className)}>
      <header className="oc-widget__header">
        {Icon && (
          <span className="oc-widget__icon" aria-hidden="true">
            <Icon size={20} />
          </span>
        )}
        <h2 className="oc-widget__title">{title}</h2>
        {action && <div className="oc-widget__action">{action}</div>}
      </header>
      <div className="oc-widget__body">{children}</div>
      {footer && <p className="oc-widget__footer">{footer}</p>}
    </section>
  );
}
