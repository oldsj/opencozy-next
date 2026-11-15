import { Fragment, type AnchorHTMLAttributes, type HTMLAttributes, type ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';

import { cx } from '../cx';

export interface TabBarItem extends Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'children' | 'href'> {
  href: string;
  label: string;
  icon: LucideIcon;
  /** The page being shown: marked aria-current="page". */
  current?: boolean;
}

/** What renderLink receives: spread it onto the router's link (it is all plain anchor props). */
export type TabBarLinkProps = Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'href'> & { href: string; children: ReactNode };

export interface TabBarProps extends HTMLAttributes<HTMLElement> {
  items: readonly TabBarItem[];
  /** Accessible name of the navigation landmark. */
  label?: string;
  /**
   * Renders each item's link, for client-side navigation:
   * `renderLink={({ href, ...p }) => <Link to={href} {...p} />}`. Default: a plain <a>.
   */
  renderLink?: (props: TabBarLinkProps) => ReactNode;
}

const plainLink = (props: TabBarLinkProps) => <a {...props} />;

/** The family navigation: icon plus label, 64px targets. */
export function TabBar({ className, items, label = 'Family navigation', renderLink = plainLink, ...props }: TabBarProps) {
  return (
    <nav {...props} className={cx('oc-tab-bar', className)} aria-label={label}>
      <div className="oc-tab-bar__items">
        {items.map(({ className: itemClassName, current = false, href, icon: Icon, label: itemLabel, ...itemProps }) => (
          <Fragment key={href}>
            {renderLink({
              ...itemProps,
              href,
              className: cx('oc-tab-bar__item', current && 'oc-tab-bar__item--current', itemClassName),
              'aria-current': current ? 'page' : undefined,
              children: (
                <>
                  <Icon aria-hidden="true" size={24} strokeWidth={2} />
                  <span>{itemLabel}</span>
                </>
              ),
            })}
          </Fragment>
        ))}
      </div>
    </nav>
  );
}
