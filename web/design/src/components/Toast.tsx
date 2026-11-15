import type { HTMLAttributes, ReactNode } from 'react';
import { Check } from 'lucide-react';

import { cx } from '../cx';

export interface ToastProps extends HTMLAttributes<HTMLDivElement> {
  children: ReactNode;
}

/** A brief confirmation ("Saved"), announced politely. Never the only record of an error. */
export function Toast({ children, className, ...props }: ToastProps) {
  return (
    <div {...props} className={cx('oc-toast', className)} role="status">
      <span className="oc-toast__icon" aria-hidden="true">
        <Check size={20} strokeWidth={2.5} />
      </span>
      <span>{children}</span>
    </div>
  );
}
