// @dsCard group="Actions"
import { ButtonSecondary } from '../../src';

export function Default() {
  return <ButtonSecondary>Save for later</ButtonSecondary>;
}

export function FullWidth() {
  return (
    <div className="max-w-xs">
      <ButtonSecondary fullWidth>Skip for now</ButtonSecondary>
    </div>
  );
}
