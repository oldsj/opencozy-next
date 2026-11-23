import { describe, expect, it } from 'vitest';

import { fromWeekdays, minutesLabel, openHours, parseMacs, secondsUntil, toWeekdays, whoChoices, type TvChild } from './tv';

const kid = (over: Partial<TvChild>): TvChild => ({
  childId: 'c1', name: 'Abby', color: 'sky', tvSecondsRemaining: 1500, watchingTvId: null, hours: null, canWatch: true, canStartOwn: false, ...over,
});

describe('whoChoices', () => {
  it('offers parents, each child with their time, open and not now', () => {
    const c = whoChoices('tv1', [kid({}), kid({ childId: 'c2', name: 'Ben', tvSecondsRemaining: 0 })], { open: true, notNow: true });
    expect(c.map((x) => [x.value, x.detail, x.disabled ?? false])).toEqual([
      ['parents', undefined, false],
      ['child:c1', '25 min', false],
      ['child:c2', 'no time', true],
      ['open', undefined, false],
      ['not_now', undefined, false],
    ]);
  });
  it('says why a child cannot start', () => {
    const later = new Date(2026, 10, 21, 16, 0).toISOString();
    const [, other, closed, here] = whoChoices('tv1', [
      kid({ watchingTvId: 'tv2' }),
      kid({ childId: 'c2', hours: { open: false, opensAt: later } }),
      kid({ childId: 'c3', watchingTvId: 'tv1' }),
    ]);
    expect(other).toMatchObject({ detail: 'on another TV', disabled: true });
    expect(closed?.detail).toMatch(/^from /);
    expect(closed?.disabled).toBe(true);
    expect(here?.disabled).toBe(false);
  });
});

describe('labels and times', () => {
  it('rounds minutes down, never promising more than is left', () => {
    expect([0, 30, 60, 119, 3600, 3900].map(minutesLabel)).toEqual(['no time', 'under 1 min', '1 min', '1 min', '1 hr', '1 hr 5 min']);
  });
  it('counts down against the server clock', () => {
    const end = new Date(100_000).toISOString();
    expect(secondsUntil(end, 0, 40_000)).toBe(60);
    expect(secondsUntil(end, 10_000, 40_000)).toBe(50); // the server is 10 s ahead
    expect(secondsUntil(end, 0, 200_000)).toBe(0);
  });
  it('reads MACs one per line or comma', () => {
    expect(parseMacs(' 02:00:5e:10:00:01\n02-00-5E-10-00-02, \n')).toEqual(['02:00:5e:10:00:01', '02-00-5E-10-00-02']);
  });
  it('sends every day as no days', () => {
    expect(toWeekdays([])).toBeUndefined();
    expect(toWeekdays([0, 1, 2, 3, 4, 5, 6])).toBeUndefined();
    expect(openHours('21:00', '07:00', [5, 6])).toEqual({ start: '21:00', end: '07:00', days: ['fri', 'sat'] });
    expect(openHours('21:00', '07:00', [])).toEqual({ start: '21:00', end: '07:00' });
    expect(fromWeekdays(['sat', 'fri'])).toEqual([5, 6]);
  });
});
