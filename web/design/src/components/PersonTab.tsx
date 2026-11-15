import type { ButtonHTMLAttributes } from 'react';

import { cx } from '../cx';
import { initialOf, profileClass, type ProfileColor } from '../profile-colors';

export interface PersonTabProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  /** The person's readable name; colour never carries identity alone. */
  name: string;
  /** Confirmed point balance, beside the name. */
  points?: number;
  /** Whether this tab shows the person whose content is visible. */
  selected?: boolean;
  /** The person's profile colour. */
  color?: ProfileColor;
}

/** A person selector: render inside an element with role="tablist". */
export function PersonTab({ className, color = 'butter', name, points, selected = false, type = 'button', ...props }: PersonTabProps) {
  return (
    <button
      {...props}
      className={cx('oc-person-tab', profileClass(color), selected && 'oc-person-tab--selected', className)}
      type={type}
      role="tab"
      aria-selected={selected}
    >
      <span className="oc-person-tab__mark" aria-hidden="true">
        {initialOf(name)}
      </span>
      <span className="oc-person-tab__name">{name}</span>
      {points !== undefined && (
        <strong className="oc-person-tab__points">
          {points} <small>pts</small>
        </strong>
      )}
    </button>
  );
}
