import type { FieldsetHTMLAttributes, InputHTMLAttributes, ReactNode } from 'react';

import { cx } from '../cx';

export interface CheckboxProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> {
  /** What ticking it means, in a few words. The whole row (at least 44px) is the target. */
  label: ReactNode;
  /** A quieter line under the label. */
  hint?: ReactNode;
}

/** A checkbox with its label: the whole row is tappable. For several, put them in a CheckboxGroup. */
export function Checkbox({ className, hint, label, ...props }: CheckboxProps) {
  return (
    <label className={cx('oc-checkbox', props.disabled && 'oc-checkbox--disabled', className)}>
      <input {...props} type="checkbox" className="oc-checkbox__input" />
      <span className="oc-checkbox__text">
        <span className="oc-checkbox__label">{label}</span>
        {hint !== undefined && <span className="oc-checkbox__hint">{hint}</span>}
      </span>
    </label>
  );
}

export interface CheckboxGroupProps extends Omit<FieldsetHTMLAttributes<HTMLFieldSetElement>, 'children'> {
  /** The question the boxes answer ("Shows"), read before each box. */
  legend: ReactNode;
  /** A line under the legend. */
  description?: ReactNode;
  children: ReactNode;
}

/** Several checkboxes under one legend (a fieldset), stacked. */
export function CheckboxGroup({ children, className, description, legend, ...props }: CheckboxGroupProps) {
  return (
    <fieldset {...props} className={cx('oc-checkbox-group', className)}>
      <legend className="oc-checkbox-group__legend">{legend}</legend>
      {description !== undefined && <p className="oc-checkbox-group__description">{description}</p>}
      <div className="oc-checkbox-group__items">{children}</div>
    </fieldset>
  );
}
