import type { HTMLAttributes } from 'react';

import { cx } from '../cx';
import { initialOf, profileClass, type ProfileColor } from '../profile-colors';

export interface ProfileAvatarProps extends HTMLAttributes<HTMLSpanElement> {
  /** The person's name: the initial shown and the accessible name. */
  name: string;
  color?: ProfileColor;
  size?: 'small' | 'medium' | 'large';
}

export function ProfileAvatar({ className, color = 'sage', name, size = 'medium', ...props }: ProfileAvatarProps) {
  return (
    <span
      {...props}
      className={cx('oc-profile-avatar', profileClass(color), `oc-profile-avatar--${size}`, className)}
      role="img"
      aria-label={name}
    >
      {initialOf(name)}
    </span>
  );
}
