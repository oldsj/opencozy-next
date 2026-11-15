import type { ProgressHTMLAttributes } from 'react';

import { cx } from '../cx';

export interface ProgressBarProps extends Omit<ProgressHTMLAttributes<HTMLProgressElement>, 'value' | 'max'> {
  value: number;
  max: number;
  /** Accessible name, never shown: the surrounding text states the progress. */
  label: string;
}

/** A native progress bar in the current profile's colour. */
export function ProgressBar({ className, label, max, value, ...props }: ProgressBarProps) {
  const safeMax = Math.max(1, max);
  const safeValue = Math.min(safeMax, Math.max(0, value));
  return <progress {...props} className={cx('oc-progress', className)} aria-label={label} max={safeMax} value={safeValue} />;
}
