// @dsCard group="Today"
import { ClipboardCheck, Moon, Sunrise } from 'lucide-react';

import { PersonCard, TaskGroup, TaskRow } from '../../src';

export function States() {
  return (
    <div className="max-w-md">
      <PersonCard name="Ezra" color="sage" points={18} completed={3} total={6}>
        <TaskGroup title="Morning" icon={Sunrise} done />
        <TaskGroup title="Bedtime" icon={Moon} status="4 points">
          <TaskRow title="Brush teeth" check={{ status: 'complete', checked: true, label: 'Brush teeth' }} />
          <TaskRow title="Pyjamas on" check={{ status: 'sending', checked: false, label: 'Saving: Pyjamas on' }} />
          <TaskRow title="Read a story" check={{ status: 'open', checked: false, label: 'Read a story' }} />
        </TaskGroup>
        <TaskGroup title="Chores" icon={ClipboardCheck}>
          <TaskRow title="Tidy the hallway" points={5} status="pending" />
          <TaskRow
            title="Water the plants"
            points={3}
            message="Not approved yet: the pots by the window still look dry, and the basil on the back step needs some too. Have another go before dinner, then tick it again."
            check={{ status: 'open', checked: false, label: 'Water the plants' }}
          />
        </TaskGroup>
      </PersonCard>
    </div>
  );
}

export function Earned() {
  return (
    <div className="max-w-md">
      <PersonCard name="Ivy" color="rose" points={30} completed={2} total={2}>
        <TaskGroup title="After school" icon={Sunrise} status="Points earned" earned>
          <TaskRow title="Unpack lunchbox" check={{ status: 'complete', checked: true, label: 'Unpack lunchbox' }} />
          <TaskRow title="Homework" check={{ status: 'complete', checked: true, label: 'Homework' }} />
        </TaskGroup>
      </PersonCard>
    </div>
  );
}

export function WaitingForApproval() {
  return (
    <div className="max-w-md">
      <PersonCard name="Noor" color="butter" points={12} completed={2} total={2}>
        <TaskGroup title="Morning" icon={Sunrise} pending>
          <TaskRow title="Make the bed" status="pending" />
          <TaskRow title="Feed the fish" status="pending" />
        </TaskGroup>
        <TaskGroup
          title="Bedtime"
          icon={Moon}
          status="4 points"
          message="Not approved yet: teeth first, then pyjamas. Try again."
        >
          <TaskRow title="Brush teeth" check={{ status: 'open', checked: false, label: 'Brush teeth' }} />
        </TaskGroup>
      </PersonCard>
    </div>
  );
}
