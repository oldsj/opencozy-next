import type { HTMLAttributes } from 'react';
import { Star } from 'lucide-react';

import { cx } from '../cx';

export interface PointsBadgeProps extends HTMLAttributes<HTMLSpanElement> {
  points: number;
  /** The unit after the number. */
  label?: string;
}

/** A small pill with a star: "12 points". */
export function PointsBadge({ className, label = 'points', points, ...props }: PointsBadgeProps) {
  return (
    <span {...props} className={cx('oc-points-badge', className)}>
      <Star aria-hidden="true" size={15} strokeWidth={2.2} />
      <strong>{points}</strong> <span>{label}</span>
    </span>
  );
}
