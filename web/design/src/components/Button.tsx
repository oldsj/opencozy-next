import type { AnchorHTMLAttributes, ButtonHTMLAttributes } from 'react';

import { cx } from '../cx';

export type ButtonVariant = 'primary' | 'secondary' | 'quiet' | 'danger';

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  /**
   * `primary` for the one main action, `secondary` for alternatives, `quiet` for repeated row
   * actions such as Edit, `danger` for actions that cancel, archive or end something (outlined,
   * never a filled red block).
   */
  variant?: ButtonVariant;
  /** Fill the parent's width, for narrow or stacked layouts. */
  fullWidth?: boolean;
}

function classes(variant: ButtonVariant, fullWidth: boolean, className: string | undefined) {
  return cx('oc-button', `oc-button--${variant}`, fullWidth && 'oc-button--full', className);
}

/** A button, at least 48px tall. `type` defaults to "button", so it never submits by accident;
 * `variant` defaults to secondary, so the one primary action per view is always explicit. */
export function Button({ className, fullWidth = false, type = 'button', variant = 'secondary', ...props }: ButtonProps) {
  return <button {...props} type={type} className={classes(variant, fullWidth, className)} />;
}

export type ButtonPrimaryProps = Omit<ButtonProps, 'variant'>;

export function ButtonPrimary(props: ButtonPrimaryProps) {
  return <Button {...props} variant="primary" />;
}

export type ButtonSecondaryProps = Omit<ButtonProps, 'variant'>;

export function ButtonSecondary(props: ButtonSecondaryProps) {
  return <Button {...props} variant="secondary" />;
}

export interface ButtonLinkProps extends AnchorHTMLAttributes<HTMLAnchorElement> {
  variant?: ButtonVariant;
  fullWidth?: boolean;
}

/** A link that navigates but looks like a button, e.g. "Adult sign-in". */
export function ButtonLink({ className, fullWidth = false, variant = 'secondary', ...props }: ButtonLinkProps) {
  return <a {...props} className={classes(variant, fullWidth, className)} />;
}
