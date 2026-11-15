import type { HTMLAttributes } from 'react';
import { Star } from 'lucide-react';

import { cx } from '../cx';
import { pointsWord } from '../points';

export interface PointsBadgeProps extends HTMLAttributes<HTMLSpanElement> {
  points: number;
  /** The unit after the number; "point" or "points" by default. */
  label?: string | undefined;
}

/** A small pill with a star: "12 points", "1 point". */
export function PointsBadge({ className, label, points, ...props }: PointsBadgeProps) {
  return (
    <span {...props} className={cx('oc-points-badge', className)}>
      <Star aria-hidden="true" size={15} strokeWidth={2.2} />
      <strong>{points}</strong> <span>{label ?? pointsWord(points)}</span>
    </span>
  );
}
