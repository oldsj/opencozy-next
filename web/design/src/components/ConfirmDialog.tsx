import type { ReactNode } from 'react';

import { Button } from './Button';
import { Sheet } from './Sheet';

export interface ConfirmDialogProps {
  open: boolean;
  /** The question, naming the thing: "Archive Tablet time?". */
  title: ReactNode;
  /** What happens, in plain words: "Children won’t see it. History keeps it." */
  description?: ReactNode;
  /** The verb that does it: "Archive", "Cancel and refund", "Turn off the TV". */
  confirmLabel: string;
  /** The way out, which changes nothing. */
  cancelLabel?: string;
  /** `danger` for actions that archive, cancel or end something (an outlined red button). */
  tone?: 'default' | 'danger';
  /** While the request runs: the confirm button is disabled and says so. */
  pending?: boolean;
  /** Recovery copy when the action failed; the dialog stays open. */
  error?: ReactNode;
  onConfirm: () => void;
  onCancel: () => void;
  children?: ReactNode;
}

/**
 * Asks before an action that is hard to undo. Escape, the close button, the backdrop and the cancel
 * button all cancel; only the confirm button acts.
 */
export function ConfirmDialog({
  cancelLabel = 'Keep it',
  children,
  confirmLabel,
  description,
  error,
  onCancel,
  onConfirm,
  open,
  pending = false,
  title,
  tone = 'default',
}: ConfirmDialogProps) {
  return (
    <Sheet
      open={open}
      onClose={onCancel}
      title={title}
      description={description}
      className="oc-confirm-dialog"
      role="alertdialog"
      footer={
        <>
          <Button onClick={onCancel}>{cancelLabel}</Button>
          <Button variant={tone === 'danger' ? 'danger' : 'primary'} onClick={onConfirm} disabled={pending} aria-busy={pending || undefined}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {children}
      {error && (
        <p className="oc-confirm-dialog__error" role="alert">
          {error}
        </p>
      )}
    </Sheet>
  );
}
