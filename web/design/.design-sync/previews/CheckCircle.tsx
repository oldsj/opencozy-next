// @dsCard group="Today"
import { CheckCircle } from '../../src';

export function States() {
  return (
    <div className="flex gap-3">
      <CheckCircle label="Feed the cat" checked={false} />
      <CheckCircle label="Saving: Feed the cat" status="sending" checked={false} />
      <CheckCircle label="Feed the cat" status="complete" checked />
      <CheckCircle label="Try again: Feed the cat" status="failed" />
    </div>
  );
}
