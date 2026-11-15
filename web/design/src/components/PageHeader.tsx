import type { HTMLAttributes, ReactNode } from 'react';

import { cx } from '../cx';

export interface PageHeaderProps extends Omit<HTMLAttributes<HTMLElement>, 'title'> {
  /** The page's one h1, matching its navigation label. No eyebrow above it. */
  title: ReactNode;
  /** One plain sentence about what the page is for. */
  description?: ReactNode;
  /** The page's main action, beside the title on wide screens. */
  actions?: ReactNode;
  /** Id for the heading, so a landmark can reference it. */
  titleId?: string;
}

export function PageHeader({ actions, className, description, title, titleId, ...props }: PageHeaderProps) {
  return (
    <header {...props} className={cx('oc-page-header', className)}>
      <div className="oc-page-header__text">
        <h1 className="oc-page-header__title" id={titleId}>
          {title}
        </h1>
        {description && <p className="oc-page-header__description">{description}</p>}
      </div>
      {actions && <div className="oc-page-header__actions">{actions}</div>}
    </header>
  );
}
