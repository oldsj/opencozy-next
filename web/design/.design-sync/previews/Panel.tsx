// @dsCard group="Layout"
import { Panel } from '../../src';

export function Default() {
  return (
    <Panel heading="Today’s responsibilities" description="Choose a person to see what is waiting.">
      <p>Three chores are ready for the family.</p>
    </Panel>
  );
}

export function Tones() {
  return (
    <div className="grid gap-4">
      <Panel tone="positive" heading="Chore confirmed" description="Ezra put away the board games.">
        <p>10 points added to Ezra’s balance.</p>
      </Panel>
      <Panel tone="attention" heading="Close to a reward" description="Ada is 8 points from Movie night." />
      <Panel tone="error" heading="Couldn’t save the chore" description="Check the connection and try again." />
      <Panel tone="info" heading="Meals are coming later" description="Meal planning isn’t ready yet." />
    </div>
  );
}
