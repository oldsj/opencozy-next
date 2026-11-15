import type { HTMLAttributes, ReactNode } from 'react';

import { cx } from '../cx';
import { profileClass, type ProfileColor } from '../profile-colors';

export interface AppFrameProps extends HTMLAttributes<HTMLDivElement> {
  /** Shared ambient header for family routes (a TopBar). */
  topBar?: ReactNode;
  /** Shared family navigation (a TabBar). */
  tabBar?: ReactNode;
  /** Id of the main landmark, the skip link's target. */
  mainId?: string;
  /** Class for route-specific layout inside the main landmark. */
  contentClassName?: string;
  /** One child's own page (/kid): the page takes their colour, with a band of it at the top. */
  person?: ProfileColor | undefined;
}

/**
 * The page shell: a skip link, the top bar, the navigation and one main landmark. On the wall
 * display (data-surface="kiosk") it is exactly the screen's height, so content such as the
 * PeopleBoard scrolls inside itself rather than the page.
 */
export function AppFrame({ children, className, contentClassName, mainId = 'main-content', person, tabBar, topBar, ...props }: AppFrameProps) {
  return (
    <div {...props} className={cx('oc-app-frame', person && 'oc-app-frame--person', person && profileClass(person), className)}>
      <a className="oc-app-frame__skip-link" href={`#${mainId}`}>
        Skip to content
      </a>
      {topBar}
      {tabBar}
      <main className={cx('oc-app-frame__content', contentClassName)} id={mainId} tabIndex={-1}>
        {children}
      </main>
    </div>
  );
}
