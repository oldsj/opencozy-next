import type { HTMLAttributes, ReactNode } from 'react';

import { cx } from '../cx';

export interface AppFrameProps extends HTMLAttributes<HTMLDivElement> {
  /** Shared ambient header for family routes (a TopBar). */
  topBar?: ReactNode;
  /** Shared family navigation (a TabBar). */
  tabBar?: ReactNode;
  /** Id of the main landmark, the skip link's target. */
  mainId?: string;
  /** Class for route-specific layout inside the main landmark. */
  contentClassName?: string;
}

/** The page shell: a skip link, the top bar, the navigation and one main landmark. */
export function AppFrame({ children, className, contentClassName, mainId = 'main-content', tabBar, topBar, ...props }: AppFrameProps) {
  return (
    <div {...props} className={cx('oc-app-frame', className)}>
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
