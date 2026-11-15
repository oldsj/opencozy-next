import type { HTMLAttributes, ReactNode } from 'react';

import { cx } from '../cx';

export interface FieldProps extends HTMLAttributes<HTMLDivElement> {
  /** Visible label. Pass the control's id as `htmlFor` so the two are linked. */
  label: ReactNode;
  htmlFor: string;
  hint?: ReactNode;
  /** One native input, select or textarea; Field styles it. */
  children: ReactNode;
}

/** Label, one native control and an optional hint, styled the same in forms and sheets. */
export function Field({ children, className, hint, htmlFor, label, ...props }: FieldProps) {
  return (
    <div {...props} className={cx('oc-field', className)}>
      <label className="oc-field__label" htmlFor={htmlFor}>
        {label}
      </label>
      {children}
      {hint && <small className="oc-field__hint">{hint}</small>}
    </div>
  );
}
