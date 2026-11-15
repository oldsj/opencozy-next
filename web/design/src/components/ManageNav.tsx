import { useState, type ComponentType, type HTMLAttributes, type ReactNode } from 'react';
import { ArrowLeft, Menu, type LucideProps } from 'lucide-react';

import { cx } from '../cx';
import { Sheet } from './Sheet';
import type { TabBarLinkProps } from './TabBar';

export interface ManageNavItem {
  href: string;
  label: string;
  icon: ComponentType<LucideProps>;
  /** The page being shown (the route, or one under it). */
  current?: boolean;
  /** A count waiting here (the inbox): shown as a badge, spoken as "3 waiting". */
  badge?: number;
}

export interface ManageNavGroup {
  label: string;
  items: readonly ManageNavItem[];
}

export interface ManageNavProps extends HTMLAttributes<HTMLElement> {
  groups: readonly ManageNavGroup[];
  /** The way back to the family screens. */
  back?: { href: string; label: string };
  /** Under the links: sign out, the adult's name. */
  footer?: ReactNode;
  /** Wrap links in the router's link, as TabBar does. Default: a plain <a>. */
  renderLink?: (props: TabBarLinkProps) => ReactNode;
  label?: string;
  /** Phone: the href of the item kept on the bar beside Menu, with its badge (the inbox). */
  shortcut?: string;
  /** Phone: the label of the button that opens every section. */
  menuLabel?: string;
}

const plainLink = (props: TabBarLinkProps) => <a {...props} />;

function Badge({ count }: { count?: number | undefined }) {
  if (!count) return null;
  return (
    <span className="oc-manage-nav__badge" aria-label={`${String(count)} waiting`}>
      {count > 99 ? '99+' : count}
    </span>
  );
}

/**
 * Adult management navigation. On a wide screen, a side rail with every group. On a phone, a bar
 * with the current section, a shortcut to the inbox (with its count) and a Menu button that opens
 * every section in a bottom sheet. Put it in a ManageShell.
 */
export function ManageNav({
  back,
  className,
  footer,
  groups,
  label = 'Adult management',
  menuLabel = 'Menu',
  renderLink = plainLink,
  shortcut,
  ...props
}: ManageNavProps) {
  const [open, setOpen] = useState(false);
  const items = groups.flatMap((g) => g.items);
  const current = items.find((i) => i.current);
  const quick = items.find((i) => i.href === shortcut);
  const CurrentIcon = current?.icon;

  const list = (onNavigate?: () => void) => (
    <>
      {back &&
        renderLink({
          href: back.href,
          className: 'oc-manage-nav__back',
          children: (
            <>
              <ArrowLeft aria-hidden="true" size={20} />
              {back.label}
            </>
          ),
        })}
      {groups.map((group) => (
        <div className="oc-manage-nav__group" key={group.label}>
          <p className="oc-manage-nav__group-label">{group.label}</p>
          <ul className="oc-manage-nav__items">
            {group.items.map(({ badge, current: isCurrent, href, icon: Icon, label: itemLabel }) => (
              <li key={href}>
                {renderLink({
                  href,
                  className: cx('oc-manage-nav__item', isCurrent && 'oc-manage-nav__item--current'),
                  'aria-current': isCurrent ? 'page' : undefined,
                  onClick: onNavigate,
                  children: (
                    <>
                      <Icon aria-hidden="true" size={20} />
                      <span className="oc-manage-nav__label">{itemLabel}</span>
                      <Badge count={badge} />
                    </>
                  ),
                })}
              </li>
            ))}
          </ul>
        </div>
      ))}
      {footer && <div className="oc-manage-nav__footer">{footer}</div>}
    </>
  );

  return (
    <nav {...props} className={cx('oc-manage-nav', className)} aria-label={label}>
      <div className="oc-manage-nav__rail">{list()}</div>
      <div className="oc-manage-nav__bar">
        <p className="oc-manage-nav__current">
          {CurrentIcon && <CurrentIcon aria-hidden="true" size={22} />}
          {current?.label}
        </p>
        {quick &&
          !quick.current &&
          renderLink({
            href: quick.href,
            className: 'oc-manage-nav__shortcut',
            children: (
              <>
                <quick.icon aria-hidden="true" size={22} />
                <span className="oc-manage-nav__label">{quick.label}</span>
                <Badge count={quick.badge} />
              </>
            ),
          })}
        <button
          type="button"
          className="oc-manage-nav__menu"
          onClick={() => {
            setOpen(true);
          }}
        >
          <Menu aria-hidden="true" size={22} />
          {menuLabel}
        </button>
      </div>
      <Sheet
        open={open}
        onClose={() => {
          setOpen(false);
        }}
        title={label}
        placement="adaptive"
        className="oc-manage-nav__sheet"
      >
        {list(() => {
          setOpen(false);
        })}
      </Sheet>
    </nav>
  );
}

export interface ManageShellProps extends HTMLAttributes<HTMLDivElement> {
  /** A ManageNav. */
  nav: ReactNode;
}

/** The adult pages' frame: the ManageNav rail beside the page on a wide screen, its bar above it on a phone. */
export function ManageShell({ children, className, nav, ...props }: ManageShellProps) {
  return (
    <div {...props} className={cx('oc-manage-shell', className)}>
      {nav}
      <main className="oc-manage-shell__main">{children}</main>
    </div>
  );
}
