import type { ElementType, HTMLAttributes, LiHTMLAttributes, ReactNode } from 'react';

import { cx } from '../cx';

export type ListProps = HTMLAttributes<HTMLUListElement>;

/** One quiet surface holding rows divided by hairlines. Never nest cards inside it. */
export function List({ className, ...props }: ListProps) {
  return <ul {...props} className={cx('oc-list', className)} />;
}

export interface ListRowProps extends Omit<LiHTMLAttributes<HTMLLIElement>, 'title'> {
  /** What the row is: a reward, a chore, a person. */
  title: ReactNode;
  /** Heading element for the title, so a row reads as part of its section. */
  titleAs?: ElementType;
  /** One line of supporting facts, usually joined with " · ". */
  meta?: ReactNode;
  /** An avatar or icon that identifies the row at a glance. */
  leading?: ReactNode;
  /** A value, a status tag, or the row's own actions. */
  trailing?: ReactNode;
  /** Move the trailing slot under the text when the list is narrow (rows with several actions). */
  stackOnNarrow?: boolean;
  /** Quieter text for archived or inactive rows. */
  muted?: boolean;
}

export function ListRow({
  children,
  className,
  leading,
  meta,
  muted = false,
  stackOnNarrow = false,
  title,
  titleAs: Title = 'h3',
  trailing,
  ...props
}: ListRowProps) {
  return (
    <li
      {...props}
      className={cx(
        'oc-list-row',
        !leading && 'oc-list-row--no-leading',
        stackOnNarrow && 'oc-list-row--stack',
        muted && 'oc-list-row--muted',
        className,
      )}
    >
      {leading && <div className="oc-list-row__leading">{leading}</div>}
      <div className="oc-list-row__main">
        <Title className="oc-list-row__title">{title}</Title>
        {meta && <div className="oc-list-row__meta">{meta}</div>}
        {children}
      </div>
      {trailing && <div className="oc-list-row__trailing">{trailing}</div>}
    </li>
  );
}
