// @dsCard group="People"
import { ProgressBar, profileClass } from '../../src';

export function Progress() {
  return (
    <div className={`${profileClass('sage')} max-w-xs`}>
      <ProgressBar value={3} max={8} label="Ezra: 3 of 8 done" />
    </div>
  );
}
