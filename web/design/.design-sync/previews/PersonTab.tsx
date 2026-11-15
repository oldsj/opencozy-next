// @dsCard group="People"
import { PersonTab } from '../../src';

export function Family() {
  return (
    <div role="tablist" aria-label="Choose a family member" className="flex flex-wrap gap-2">
      <PersonTab name="Ada" points={42} color="sky" selected />
      <PersonTab name="Ezra" points={18} color="sage" />
      <PersonTab name="Ivy" points={6} color="rose" />
      <PersonTab name="Noor" points={30} color="lilac" />
    </div>
  );
}

export function NoPoints() {
  return (
    <div role="tablist" aria-label="Choose a family member">
      <PersonTab name="Guest" color="sand" selected />
    </div>
  );
}
