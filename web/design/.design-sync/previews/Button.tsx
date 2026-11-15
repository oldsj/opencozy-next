// @dsCard group="Actions"
import { Button } from '../../src';

export function Variants() {
  return (
    <div className="flex flex-wrap gap-3">
      <Button variant="primary">Complete chore</Button>
      <Button>Save for later</Button>
      <Button variant="quiet">Edit</Button>
      <Button variant="danger">Archive reward</Button>
    </div>
  );
}

export function FullWidth() {
  return (
    <div className="max-w-xs">
      <Button variant="primary" fullWidth>
        Confirm pickup
      </Button>
    </div>
  );
}

export function Disabled() {
  return (
    <div className="flex flex-wrap gap-3">
      <Button variant="primary" disabled>
        Complete chore
      </Button>
      <Button disabled>Save for later</Button>
    </div>
  );
}
