import { useEffect, useId, useRef, type DialogHTMLAttributes, type MouseEvent, type ReactNode } from 'react';
import { X } from 'lucide-react';

import { cx } from '../cx';

export type SheetPlacement = 'adaptive' | 'right' | 'bottom';

export interface SheetProps extends Omit<DialogHTMLAttributes<HTMLDialogElement>, 'open' | 'onClose' | 'title'> {
  open: boolean;
  /** Called on the close button, Escape, or a click on the backdrop. */
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  /** Actions: the primary one last. */
  footer?: ReactNode;
  /** `adaptive` is a bottom sheet on narrow screens and a side sheet on wide ones. */
  placement?: SheetPlacement;
  closeLabel?: string;
}

/** A modal sheet on the native <dialog>: focus is trapped and restored by the browser. */
export function Sheet({ children, className, closeLabel = 'Close', description, footer, onClose, open, placement = 'adaptive', title, ...props }: SheetProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const titleId = `oc-sheet-${useId().replaceAll(':', '')}`;
  const descriptionId = description ? `${titleId}-description` : undefined;

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  function handleBackdropClick(event: MouseEvent<HTMLDialogElement>) {
    if (event.target === event.currentTarget) onClose();
  }

  if (!open) return null;

  return (
    <dialog
      {...props}
      ref={dialogRef}
      className={cx('oc-sheet', `oc-sheet--${placement}`, className)}
      aria-labelledby={titleId}
      aria-describedby={descriptionId}
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
      onClick={handleBackdropClick}
    >
      <div className="oc-sheet__surface">
        <header className="oc-sheet__header">
          <div>
            <h2 className="oc-sheet__title" id={titleId}>
              {title}
            </h2>
            {description && (
              <p className="oc-sheet__description" id={descriptionId}>
                {description}
              </p>
            )}
          </div>
          <button className="oc-sheet__close" type="button" onClick={onClose} aria-label={closeLabel}>
            <X aria-hidden="true" size={24} />
          </button>
        </header>
        <div className="oc-sheet__body">{children}</div>
        {footer && <footer className="oc-sheet__footer">{footer}</footer>}
      </div>
    </dialog>
  );
}
