import type { HTMLAttributes } from 'react';

import { cx } from '../cx';
import { countdownLabel, countdownSpoken } from '../countdown';

export interface CountdownProps extends Omit<HTMLAttributes<HTMLSpanElement>, 'children'> {
  /**
   * Whole seconds left. The app owns the clock (one tick per second for a whole section, corrected
   * for server skew) and passes the result; this component only shows it.
   */
  seconds: number;
  /**
   * `inline` sits in a sentence or a Tag ("On · Abby · 24:59 left"); `large` is the iPad's own timer;
   * `hero` fills a TV's own screen, at the clock's size.
   */
  size?: 'inline' | 'large' | 'hero' | undefined;
  /** From this many seconds left the timer is marked low (`oc-countdown--low`): stronger, in the attention ink. */
  lowAt?: number | undefined;
}

/**
 * "24:59 left", then "Time’s up" at zero. A timer that is not announced as it ticks (aria-live off):
 * a screen reader reaching it hears whole minutes ("25 minutes left").
 */
export function Countdown({ className, lowAt = 300, seconds, size = 'inline', ...props }: CountdownProps) {
  return (
    <span
      {...props}
      className={cx('oc-countdown', `oc-countdown--${size}`, seconds > 0 && seconds <= lowAt && 'oc-countdown--low', seconds <= 0 && 'oc-countdown--ended', className)}
      role="timer"
      aria-live="off"
      aria-atomic="true"
      aria-label={countdownSpoken(seconds)}
    >
      {seconds > 0 ? `${countdownLabel(seconds)} left` : 'Time’s up'}
    </span>
  );
}
