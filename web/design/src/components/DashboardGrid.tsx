import type { HTMLAttributes, ReactNode } from 'react';

import { cx } from '../cx';

// The grid is 6 columns wide and rows grow as needed: x and y are 0-based, w and h 1-based.

// Static class names (the CSP forbids style=, so placement can't be inline). Out-of-range values clamp.
const xClass = ['oc-dash-x0', 'oc-dash-x1', 'oc-dash-x2', 'oc-dash-x3', 'oc-dash-x4', 'oc-dash-x5'] as const;
const yClass = ['oc-dash-y0', 'oc-dash-y1', 'oc-dash-y2', 'oc-dash-y3', 'oc-dash-y4', 'oc-dash-y5', 'oc-dash-y6', 'oc-dash-y7', 'oc-dash-y8', 'oc-dash-y9', 'oc-dash-y10', 'oc-dash-y11', 'oc-dash-y12', 'oc-dash-y13', 'oc-dash-y14', 'oc-dash-y15', 'oc-dash-y16', 'oc-dash-y17', 'oc-dash-y18', 'oc-dash-y19', 'oc-dash-y20', 'oc-dash-y21', 'oc-dash-y22', 'oc-dash-y23'] as const;
const wClass = ['oc-dash-w1', 'oc-dash-w2', 'oc-dash-w3', 'oc-dash-w4', 'oc-dash-w5', 'oc-dash-w6'] as const;
const hClass = ['oc-dash-h1', 'oc-dash-h2', 'oc-dash-h3', 'oc-dash-h4', 'oc-dash-h5', 'oc-dash-h6'] as const;

function pick(list: readonly string[], index: number): string {
  const i = Math.min(Math.max(Math.trunc(index), 0), list.length - 1);
  return list[i] ?? '';
}

export interface DashboardGridProps extends HTMLAttributes<HTMLDivElement> {
  /** Editing: items show their outline, and an empty grid shows its cells. */
  editing?: boolean | undefined;
}

/**
 * A dashboard view's layout: widgets placed on a 6-column grid at their saved x, y, w and h. Below
 * 1024px it stacks in one column, so put children in reading order (by y, then x). Give it the full
 * width of the screen: the breakpoint reads the viewport (kiosk browsers lack container queries).
 */
export function DashboardGrid({ className, editing = false, ...props }: DashboardGridProps) {
  return <div {...props} className={cx('oc-dash', editing && 'oc-dash--editing', className)} />;
}

export interface DashboardItemProps extends HTMLAttributes<HTMLDivElement> {
  /** Column, 0 to 5. */
  x: number;
  /** Row, from 0. */
  y: number;
  /** Width in columns, 1 to 6. */
  w: number;
  /** Height in rows, 1 to 6. */
  h: number;
  /** In the editor: the item being moved or resized. */
  selected?: boolean | undefined;
  children?: ReactNode;
}

export function DashboardItem({ className, h, selected = false, w, x, y, ...props }: DashboardItemProps) {
  return (
    <div
      {...props}
      className={cx('oc-dash__item', pick(xClass, x), pick(yClass, y), pick(wClass, w - 1), pick(hClass, h - 1), selected && 'oc-dash__item--selected', className)}
    />
  );
}
