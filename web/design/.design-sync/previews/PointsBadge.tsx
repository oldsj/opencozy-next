// @dsCard group="People"
import { PointsBadge } from '../../src';

export function Values() {
  return (
    <div className="flex items-center gap-3">
      <PointsBadge points={4} />
      <PointsBadge points={24} />
      <PointsBadge points={150} />
    </div>
  );
}

export function InContext() {
  return (
    <div className="flex items-center gap-2">
      <strong className="font-heading">Ada</strong>
      <PointsBadge points={24} />
    </div>
  );
}
