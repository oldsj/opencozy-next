import { useId, type InputHTMLAttributes, type ReactNode, type TextareaHTMLAttributes } from 'react';

import { cx } from '../cx';

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  /** Visible label naming the value. Placeholders are not labels. */
  label?: ReactNode;
  /** Guidance shown below the control and read with it. */
  hint?: ReactNode;
  /** Recovery copy; marks the input invalid and is announced. */
  error?: ReactNode;
}

interface FieldText {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
}

/** Ids and ARIA wiring shared by Input and Textarea. */
function useFieldIds(id: string | undefined, { error, hint }: FieldText, describedBy: string | undefined) {
  const generated = useId();
  const controlId = id ?? `oc-input-${generated.replaceAll(':', '')}`;
  const hintId = hint ? `${controlId}-hint` : undefined;
  const errorId = error ? `${controlId}-error` : undefined;
  return {
    controlId,
    hintId,
    errorId,
    describedBy: [describedBy, hintId, errorId].filter(Boolean).join(' ') || undefined,
  };
}

function FieldFrame({ children, controlId, error, errorId, hint, hintId, label }: FieldText & { children: ReactNode; controlId: string; hintId?: string | undefined; errorId?: string | undefined }) {
  return (
    <div className="oc-input-field">
      {label && (
        <label className="oc-input-field__label" htmlFor={controlId}>
          {label}
        </label>
      )}
      {children}
      {hint && (
        <small id={hintId} className="oc-input-field__hint">
          {hint}
        </small>
      )}
      {error && (
        <small id={errorId} className="oc-input-field__error" role="alert">
          {error}
        </small>
      )}
    </div>
  );
}

/** A labelled text input with an optional hint and error. */
export function Input({ className, error, hint, id, label, ...props }: InputProps) {
  const ids = useFieldIds(id, { error, hint }, props['aria-describedby']);
  return (
    <FieldFrame {...ids} label={label} hint={hint} error={error}>
      <input
        {...props}
        id={ids.controlId}
        className={cx('oc-input', className)}
        aria-describedby={ids.describedBy}
        aria-invalid={props['aria-invalid'] ?? (error ? true : undefined)}
      />
    </FieldFrame>
  );
}

export interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement>, FieldText {}

/** A labelled multi-line input, e.g. a reward's description. Same wiring as Input. */
export function Textarea({ className, error, hint, id, label, rows = 4, ...props }: TextareaProps) {
  const ids = useFieldIds(id, { error, hint }, props['aria-describedby']);
  return (
    <FieldFrame {...ids} label={label} hint={hint} error={error}>
      <textarea
        {...props}
        rows={rows}
        id={ids.controlId}
        className={cx('oc-input', 'oc-input--multiline', className)}
        aria-describedby={ids.describedBy}
        aria-invalid={props['aria-invalid'] ?? (error ? true : undefined)}
      />
    </FieldFrame>
  );
}
