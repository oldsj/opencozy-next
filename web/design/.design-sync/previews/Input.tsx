// @dsCard group="Forms"
import { Input } from '../../src';

export function Default() {
  return <Input label="Chore name" placeholder="e.g. Feed the cat" hint="Use a short name everyone can recognize." />;
}

export function WithError() {
  return <Input label="Point value" defaultValue="abc" error="Enter a whole number of points." />;
}

export function Disabled() {
  return <Input label="Household name" defaultValue="Maple House" disabled />;
}
