import type { SVGAttributes } from 'react';

import { cx } from '../cx';

export interface QrCodeProps extends Omit<SVGAttributes<SVGSVGElement>, 'children'> {
  /**
   * The QR symbol's modules, row by row: true is a dark module. The caller encodes (or the server
   * returns) the matrix; this component only draws it, so no QR library or canvas is needed and
   * nothing is injected as markup.
   */
  modules: readonly (readonly boolean[])[];
  /** Accessible name, e.g. "Pairing code for the kitchen display". */
  label: string;
  /** Light modules around the symbol; the QR spec asks for 4. */
  quietZone?: number;
}

/** Draws a QR code from its module matrix as one SVG path, crisp at any size. */
export function QrCode({ className, label, modules, quietZone = 4, ...props }: QrCodeProps) {
  const size = modules.length + quietZone * 2;
  let d = '';
  modules.forEach((row, y) => {
    row.forEach((dark, x) => {
      if (dark) d += `M${String(x + quietZone)} ${String(y + quietZone)}h1v1h-1z`;
    });
  });
  return (
    <svg
      {...props}
      className={cx('oc-qr-code', className)}
      viewBox={`0 0 ${String(size)} ${String(size)}`}
      role="img"
      aria-label={label}
      shapeRendering="crispEdges"
    >
      <rect className="oc-qr-code__light" width={size} height={size} />
      <path className="oc-qr-code__dark" d={d} />
    </svg>
  );
}

/**
 * The server's QR rows ("0" light, "1" dark, one string per row) as QrCode's `modules`. Anything but
 * "1" is light, so a malformed row can't add dark modules.
 */
export function qrModulesFromRows(rows: readonly string[]): boolean[][] {
  return rows.map((row) => Array.from(row, (c) => c === '1'));
}
