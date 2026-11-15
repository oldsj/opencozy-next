import type { HTMLAttributes } from 'react';

import { cx } from '../cx';
import { Button, type ButtonVariant } from './Button';

export interface Choice<V extends string = string> {
  value: V;
  /** What the button says: "Ada", "30 min", "Not now". */
  label: string;
  /**
   * A short second part, quieter: "25 min", "no time", "from 4:00 pm". It is part of the button's
   * name ("Ada 25 min"), so voice control users can say what they see; keep it plain words.
   */
  detail?: string;
  disabled?: boolean;
  /** Defaults to secondary. One primary at most; `quiet` for the way out ("No", "Not now"). */
  variant?: ButtonVariant;
}

export interface ChoiceGroupProps<V extends string = string> extends Omit<HTMLAttributes<HTMLDivElement>, 'onChange'> {
  /** The question the buttons answer, for assistive tech: "Who’s watching?". */
  label: string;
  choices: readonly Choice<V>[];
  onChoose: (value: V) => void;
  /** An answer is being sent: every choice is disabled. */
  busy?: boolean;
}

/**
 * One-tap answers to a question, wrapping onto more lines as needed: who is watching a TV that
 * just turned on, or how much more time to give ("30 min", "1 hour", "All day", "No").
 */
export function ChoiceGroup<V extends string = string>({ busy = false, choices, className, label, onChoose, ...props }: ChoiceGroupProps<V>) {
  return (
    <div {...props} className={cx('oc-choice-group', className)} role="group" aria-label={label} aria-busy={busy || undefined}>
      {choices.map((choice) => (
        <Button
          key={choice.value}
          variant={choice.variant ?? 'secondary'}
          disabled={busy || choice.disabled}
          onClick={() => {
            onChoose(choice.value);
          }}
        >
          {choice.label}
          {choice.detail && (
            <>
              {' '}
              <span className="oc-choice-group__detail">{choice.detail}</span>
            </>
          )}
        </Button>
      ))}
    </div>
  );
}
