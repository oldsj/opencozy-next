import type { HTMLAttributes } from 'react';

import { cx } from '../cx';

const short = ['S', 'M', 'T', 'W', 'T', 'F', 'S'] as const;
const long = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'] as const;
/** Monday first, as the household week reads. */
const order = [1, 2, 3, 4, 5, 6, 0] as const;

export interface DayPickerProps extends Omit<HTMLAttributes<HTMLDivElement>, 'onChange'> {
  /** Accessible name for the group, e.g. "Days for Feed the cat". */
  label: string;
  /** Days of the week, 0 = Sunday. Empty means every day (as the API stores it). */
  value: readonly number[];
  onChange: (days: number[]) => void;
}

/**
 * Which days of the week something happens: each day toggles. Every day pressed is sent as `[]`, and
 * the last pressed day can't be turned off (something always has at least one day).
 */
export function DayPicker({ className, label, onChange, value, ...props }: DayPickerProps) {
  const on = new Set(value.length === 0 ? [0, 1, 2, 3, 4, 5, 6] : value);
  return (
    <div {...props} className={cx('oc-segmented', className)} role="group" aria-label={label}>
      {order.map((day) => {
        const pressed = on.has(day);
        return (
          <button
            key={day}
            type="button"
            className="oc-segmented__option"
            aria-pressed={pressed}
            aria-label={long[day]}
            disabled={pressed && on.size === 1}
            onClick={() => {
              const next = new Set(on);
              if (pressed) next.delete(day);
              else next.add(day);
              onChange(next.size === 7 ? [] : [...next].sort((a, b) => a - b));
            }}
          >
            {short[day]}
          </button>
        );
      })}
    </div>
  );
}
