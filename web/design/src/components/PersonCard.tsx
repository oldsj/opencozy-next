import type { HTMLAttributes, ReactNode } from 'react';

import { cx } from '../cx';
import { profileClass, type ProfileColor } from '../profile-colors';
import { PointsBadge } from './PointsBadge';
import { ProfileAvatar } from './ProfileAvatar';
import { ProgressBar } from './ProgressBar';

export interface PersonCardProps extends HTMLAttributes<HTMLElement> {
  name: string;
  color?: ProfileColor;
  points: number;
  completed: number;
  total: number;
  /** Points earned today; shown beside the progress once there are some. */
  earnedToday?: number;
  footer?: ReactNode;
}

/**
 * One person's column on Today: identity, points and progress above their tasks (children). With
 * nothing on the list (`total` 0) the progress is left out rather than showing "0 of 0".
 */
export function PersonCard({ children, className, color = 'sage', completed, earnedToday = 0, footer, name, points, total, ...props }: PersonCardProps) {
  return (
    <article {...props} className={cx('oc-person-card', profileClass(color), className)} aria-label={`${name}'s today`}>
      <header className="oc-person-card__header">
        <ProfileAvatar name={name} color={color} size="large" />
        <div className="oc-person-card__identity">
          <h2>{name}</h2>
          {total > 0 && (
            <p>
              {completed} of {total} done
              {earnedToday > 0 && (
                <>
                  {' · '}
                  <span className="oc-person-card__earned">+{earnedToday} today</span>
                </>
              )}
            </p>
          )}
        </div>
        <PointsBadge points={points} />
        {total > 0 && (
          <ProgressBar className="oc-person-card__progress" value={completed} max={total} label={`${name}: ${String(completed)} of ${String(total)} done`} />
        )}
      </header>
      <div className="oc-person-card__body">{children}</div>
      {footer && <footer className="oc-person-card__footer">{footer}</footer>}
    </article>
  );
}
