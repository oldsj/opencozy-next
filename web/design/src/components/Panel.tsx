import type { HTMLAttributes, ReactNode } from 'react';

import { cx } from '../cx';

export type PanelTone = 'default' | 'positive' | 'attention' | 'error' | 'info';

export interface PanelProps extends HTMLAttributes<HTMLElement> {
  /** Optional heading (an h2) in the editorial heading style. */
  heading?: ReactNode;
  /** Supporting copy under the heading. */
  description?: ReactNode;
  /** Surface tone for a confirmed, attention, error or information state. */
  tone?: PanelTone;
  /** The element: `li` inside a list of panels, `article` for a self-contained item. */
  as?: 'section' | 'article' | 'div' | 'li';
}

/** A calm surface grouping one meaningful task or state. Don't nest panels. */
export function Panel({ as: Element = 'section', children, className, description, heading, tone = 'default', ...props }: PanelProps) {
  return (
    <Element {...props} className={cx('oc-panel', `oc-panel--${tone}`, className)}>
      {(heading ?? description) && (
        <header className="oc-panel__header">
          {heading && <h2 className="oc-panel__heading">{heading}</h2>}
          {description && <p className="oc-panel__description">{description}</p>}
        </header>
      )}
      {children}
    </Element>
  );
}
