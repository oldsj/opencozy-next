import { useId, type HTMLAttributes, type ReactNode } from 'react';

import { cx } from '../cx';

export interface SectionProps extends Omit<HTMLAttributes<HTMLElement>, 'title'> {
  /** One h2 per section; it names the content, with no eyebrow above it. */
  title: ReactNode;
  /** Optional short note under the heading. */
  description?: ReactNode;
  /** Optional item count, shown quietly beside the heading. */
  count?: number;
  /** Controls for this section only (a filter, an add button). */
  actions?: ReactNode;
  /** Hide the heading visually, keeping it for assistive technology. */
  hideTitle?: boolean;
}

export function Section({ actions, children, className, count, description, hideTitle = false, title, ...props }: SectionProps) {
  const titleId = `oc-section-${useId().replaceAll(':', '')}`;
  return (
    <section {...props} className={cx('oc-section', className)} aria-labelledby={titleId}>
      <header className={cx('oc-section__header', hideTitle && !actions && !description && 'oc-section__header--hidden')}>
        <div className={cx('oc-section__text', hideTitle && 'oc-visually-hidden')}>
          <h2 className="oc-section__title" id={titleId}>
            {title}
            {count !== undefined && (
              <span className="oc-section__count">
                <span className="oc-visually-hidden">, </span>
                {count}
              </span>
            )}
          </h2>
          {description && <p className="oc-section__description">{description}</p>}
        </div>
        {actions && <div className="oc-section__actions">{actions}</div>}
      </header>
      {children}
    </section>
  );
}
