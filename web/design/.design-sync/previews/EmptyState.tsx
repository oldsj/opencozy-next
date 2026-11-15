// @dsCard group="Feedback"
import { CircleCheck, Gift } from 'lucide-react';

import { Button, EmptyState } from '../../src';

export function AllDone() {
  return <EmptyState icon={CircleCheck} title="All done for today" description="Your points are ready to spend in Rewards." />;
}

export function EmptyList() {
  return (
    <EmptyState
      variant="surface"
      icon={Gift}
      title="No rewards yet"
      description="Add something the children can save up for."
      action={<Button variant="primary">Add reward</Button>}
    />
  );
}
