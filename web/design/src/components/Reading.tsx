import type { ComponentType, HTMLAttributes, ReactNode } from 'react';
import type { LucideProps } from 'lucide-react';

import { cx } from '../cx';

/**
 * How current a reading is. `current` is fresh; `stale` is the last good value, shown with its age;
 * `error` has no value to show; `pending` hasn't been read yet.
 */
export type ReadingStatus = 'current' | 'stale' | 'error' | 'pending';

export interface ReadingProps extends Omit<HTMLAttributes<HTMLDivElement>, 'title'> {
  /** What it measures, in the household's words: "Kitchen", "Front door". */
  label: ReactNode;
  /** The value as text: "71", "Closed", "Locked". Ignored for error and pending. */
  value?: ReactNode;
  /** A unit after the value: "°F", "%", "kW". */
  unit?: ReactNode;
  icon?: ComponentType<LucideProps> | undefined;
  status?: ReadingStatus | undefined;
  /** A short line under the value; for stale readings, say when it's from ("As of 4:02 PM"). */
  note?: ReactNode;
  /** `attention` for a state worth a glance from across the room (a door left open). */
  tone?: 'neutral' | 'attention' | undefined;
}

/**
 * One reading from the home (Home Assistant), sized to read from across the room. Read-only: it
 * never controls anything. A stale value stays visible but quieter, and says how old it is; a value
 * that can't be read says so rather than showing a blank or a zero. Lay several out in a
 * `<div className="oc-readings">`: as many 12rem tiles per row as fit.
 */
export function Reading({ className, icon: Icon, label, note, status = 'current', tone = 'neutral', unit, value, ...props }: ReadingProps) {
  const shown = status === 'error' ? 'Can’t read' : status === 'pending' ? 'Checking…' : value;
  return (
    <div
      {...props}
      className={cx('oc-reading', `oc-reading--${status}`, tone === 'attention' && status !== 'error' && 'oc-reading--attention', className)}
    >
      <p className="oc-reading__label">
        {Icon && <Icon size={18} aria-hidden="true" className="oc-reading__icon" />}
        <span>{label}</span>
      </p>
      <p className="oc-reading__value">
        {shown}
        {unit !== undefined && (status === 'current' || status === 'stale') && <span className="oc-reading__unit">{unit}</span>}
      </p>
      {note !== undefined && <p className="oc-reading__note">{note}</p>}
    </div>
  );
}
