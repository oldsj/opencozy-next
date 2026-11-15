import type { HTMLAttributes } from 'react';
import type { LucideIcon } from 'lucide-react';

import { cx } from '../cx';

export interface ComingLaterTileProps extends HTMLAttributes<HTMLElement> {
  title: string;
  icon: LucideIcon;
  description?: string;
}

/** A planned feature, shown honestly as not ready yet. Never interactive. */
export function ComingLaterTile({ className, description, icon: Icon, title, ...props }: ComingLaterTileProps) {
  return (
    <article {...props} className={cx('oc-coming-later-tile', className)}>
      <span className="oc-coming-later-tile__icon" aria-hidden="true">
        <Icon size={24} strokeWidth={2} />
      </span>
      <div className="oc-coming-later-tile__copy">
        <h3>{title}</h3>
        {description && <p>{description}</p>}
      </div>
      <span className="oc-coming-later-tile__status">Coming later</span>
    </article>
  );
}
