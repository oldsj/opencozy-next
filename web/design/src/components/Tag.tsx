import type { HTMLAttributes } from 'react';

import { cx } from '../cx';

export type TagTone = 'neutral' | 'attention' | 'positive' | 'info' | 'error';

export interface TagProps extends HTMLAttributes<HTMLSpanElement> {
  /** Tag only a state that differs from normal (Canceled, Out of date); the normal case has none. */
  tone?: TagTone;
}

export function Tag({ className, tone = 'neutral', ...props }: TagProps) {
  return <span {...props} className={cx('oc-tag', `oc-tag--${tone}`, className)} />;
}
