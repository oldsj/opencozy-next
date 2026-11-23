// @dsCard group="Today"
import { TaskRow } from '../../src';

export function Open() {
  return <TaskRow title="Put away the board games" assignee="Ezra" points={10} metadata="Today" onAction={() => undefined} />;
}

export function Pending() {
  return <TaskRow title="Feed the cat" assignee="Abby" points={5} metadata="Today · morning" status="pending" onAction={() => undefined} />;
}

export function Complete() {
  return <TaskRow title="Empty the dishwasher" assignee="Ezra" points={8} metadata="Yesterday" status="complete" />;
}

export function CheckStates() {
  return (
    <div className="grid gap-3">
      <TaskRow title="Put away books" points={5} check={{ status: 'open', checked: false, label: 'Put away books' }} />
      <TaskRow title="Put on shoes" points={2} check={{ status: 'sending', checked: false, label: 'Saving: Put on shoes' }} />
      <TaskRow title="Make the bed" points={3} check={{ status: 'complete', checked: true, label: 'Make the bed' }} />
      <TaskRow title="Feed the cat" points={4} error="Couldn’t save — tap to try again." check={{ status: 'failed', label: 'Try again: Feed the cat' }} />
    </div>
  );
}
