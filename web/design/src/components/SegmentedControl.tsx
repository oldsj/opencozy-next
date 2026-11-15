import type { HTMLAttributes, ReactNode } from 'react';

import { cx } from '../cx';

export interface SegmentedOption<Value extends string> {
  value: Value;
  label: ReactNode;
}

export interface SegmentedControlProps<Value extends string> extends Omit<HTMLAttributes<HTMLDivElement>, 'onChange'> {
  /** Accessible name for the group, e.g. "Show rewards". */
  label: string;
  options: readonly SegmentedOption<Value>[];
  value: Value;
  onChange: (value: Value) => void;
}

/** A single choice between two to five views of one section. Never wraps; scrolls sideways instead. */
export function SegmentedControl<Value extends string>({ className, label, onChange, options, value, ...props }: SegmentedControlProps<Value>) {
  return (
    <div {...props} className={cx('oc-segmented', className)} role="group" aria-label={label}>
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          className="oc-segmented__option"
          aria-pressed={option.value === value}
          onClick={() => {
            onChange(option.value);
          }}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}
