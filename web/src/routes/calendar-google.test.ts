import { describe, expect, it } from 'vitest';

import { profileColors } from '@app/design';

import { googleOutcomes, googleOutcomeText, nextColor } from './calendar-google';

describe('nextColor', () => {
  it('takes the first colour no calendar uses', () => {
    expect(nextColor([])).toBe(profileColors[0]);
    expect(nextColor([profileColors[0], profileColors[2]])).toBe(profileColors[1]);
  });

  it('goes round in turn once every colour is used', () => {
    expect(nextColor([...profileColors])).toBe(profileColors[0]);
    expect(nextColor([...profileColors, profileColors[3]])).toBe(profileColors[1]);
  });
});

describe('googleOutcomeText', () => {
  it('has a sentence for every way back but success', () => {
    for (const o of googleOutcomes) {
      if (o !== 'connected') expect(googleOutcomeText[o]).toMatch(/\.$/);
    }
  });
});
