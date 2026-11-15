/** "point" for exactly one (or minus one), "points" otherwise: 0 points, 1 point, −1 point, 12 points. */
export function pointsWord(points: number): 'point' | 'points' {
  return Math.abs(points) === 1 ? 'point' : 'points';
}

/** "1 point", "12 points". Pass a prefix such as "+3" or "−3" through `shown` to keep its sign. */
export function pointsLabel(points: number, shown: string = String(points)): string {
  return `${shown} ${pointsWord(points)}`;
}
