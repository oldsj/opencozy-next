// @dsCard group="Forms"
import { Checkbox, CheckboxGroup } from '../../src';

/** What a wall display shows (Manage → Displays). */
export function DisplayShows() {
  return (
    <CheckboxGroup legend="Shows" description="Tabs along the bottom of the display, in this order.">
      <Checkbox label="Today" defaultChecked />
      <Checkbox label="TV" />
      <Checkbox label="Calendar" defaultChecked />
      <Checkbox label="Meals" />
      <Checkbox label="Dashboard" hint="Make a dashboard view first." disabled />
    </CheckboxGroup>
  );
}
