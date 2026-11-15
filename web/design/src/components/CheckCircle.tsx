import type { ButtonHTMLAttributes } from 'react';
import { Check, LoaderCircle, RotateCcw } from 'lucide-react';

import { cx } from '../cx';

export type CheckCircleStatus = 'open' | 'sending' | 'complete' | 'failed';

export interface CheckCircleProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children'> {
  status?: CheckCircleStatus;
  /** Supplying `checked` gives the control checkbox semantics. A failed check is a retry button. */
  checked?: boolean;
  /** Accessible name, e.g. "Feed the cat". */
  label: string;
}

/** The 56px completion control in child-facing task lists. */
export function CheckCircle({ checked, className, disabled, label, onClick, status = 'open', type = 'button', ...props }: CheckCircleProps) {
  const isCheckbox = checked !== undefined && status !== 'failed';
  return (
    <button
      {...props}
      type={type}
      onClick={onClick}
      className={cx('oc-check-circle', `oc-check-circle--${status}`, className)}
      aria-label={label}
      aria-busy={status === 'sending' || undefined}
      aria-checked={isCheckbox ? checked : undefined}
      role={isCheckbox ? 'checkbox' : undefined}
      disabled={Boolean(disabled) || status === 'sending' || (status === 'complete' && !onClick)}
    >
      {status === 'sending' && <LoaderCircle aria-hidden="true" size={25} strokeWidth={2.2} />}
      {status === 'complete' && <Check aria-hidden="true" size={27} strokeWidth={2.5} />}
      {status === 'failed' && <RotateCcw aria-hidden="true" size={23} strokeWidth={2.2} />}
    </button>
  );
}
