// @dsCard group="Forms"
import { Checkbox } from '../../src';

export function Default() {
  return <Checkbox label="Remember this device" defaultChecked />;
}

export function WithHint() {
  return <Checkbox label="Meals" hint="Today’s meal plan, after the board." />;
}

export function Disabled() {
  return <Checkbox label="Dashboard" hint="Make a dashboard view first." disabled />;
}
