// @dsCard group="Today"
import { ClipboardCheck, Sunrise } from 'lucide-react';

import { EmptyState, PeopleBoard, PersonCard, TaskGroup, TaskRow } from '../../src';

export function ThreePeople() {
  return (
    <PeopleBoard>
      <PersonCard name="Ada" color="sky" points={24} completed={1} total={2}>
        <TaskGroup title="Morning" icon={Sunrise} status="3 points">
          <TaskRow title="Make the bed" check={{ status: 'complete', checked: true, label: 'Make the bed' }} />
          <TaskRow title="Pack school bag" check={{ status: 'open', checked: false, label: 'Pack school bag' }} />
        </TaskGroup>
      </PersonCard>
      <PersonCard name="Ezra" color="sage" points={18} completed={0} total={1}>
        <TaskGroup title="Chores" icon={ClipboardCheck}>
          <TaskRow
            title="Feed the cat"
            points={4}
            error="Couldn’t save — tap to try again."
            check={{ status: 'failed', label: 'Try again: Feed the cat' }}
          />
        </TaskGroup>
      </PersonCard>
      <PersonCard name="Noor" color="lilac" points={6} completed={0} total={0}>
        <EmptyState title="Nothing on the list today" description="When a routine or chore is ready, it will appear here." />
      </PersonCard>
    </PeopleBoard>
  );
}
