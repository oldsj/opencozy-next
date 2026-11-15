// @dsCard group="Actions"
import { ButtonPrimary } from '../../src';

export function Default() {
  return <ButtonPrimary>Complete chore</ButtonPrimary>;
}

export function Disabled() {
  return <ButtonPrimary disabled>Complete chore</ButtonPrimary>;
}
