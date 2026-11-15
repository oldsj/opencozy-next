// @dsCard group="Feedback"
import { StatusPill } from '../../src';

// `live` renders nothing: a healthy connection needs no attention.
export function Degraded() {
  return (
    <div className="flex flex-wrap gap-3">
      <StatusPill status="live" />
      <StatusPill status="reconnecting" />
      <StatusPill status="offline" />
    </div>
  );
}
