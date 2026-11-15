// @dsCard group="Today"
import { AlarmClock, BrushCleaning } from 'lucide-react';

import { PersonCard, TaskRow } from '../../src';

export function Today() {
  return (
    <div className="max-w-md">
      <PersonCard
        name="Ada"
        color="sky"
        points={24}
        completed={1}
        total={3}
        earnedToday={5}
        footer={<a href="#rewards">See rewards</a>}
      >
        <TaskRow title="Brush teeth" points={2} icon={<BrushCleaning />} check={{ status: 'open', checked: false, label: 'Brush teeth' }} />
        <TaskRow title="Make the bed" points={3} check={{ status: 'complete', checked: true, label: 'Make the bed' }} />
        <TaskRow
          title="Pack school bag"
          points={3}
          icon={<AlarmClock />}
          error="Couldn’t save — tap to try again."
          check={{ status: 'failed', label: 'Try again: Pack school bag' }}
        />
      </PersonCard>
    </div>
  );
}
