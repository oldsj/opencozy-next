import { describe, expect, it } from 'vitest';

import { creditLeft, creditTitle, domainsFromText, minutesLabel, screenState, spendChoices } from './screen-time-logic';

const at = (hhmm: string) => `2026-11-18T${hhmm}:00Z`;
const now = Date.parse(at('16:00'));

describe('screenState', () => {
  it('is locked with nothing running', () => {
    expect(screenState({ windows: [], bankedSeconds: 1830 }, now)).toEqual({ runningSeconds: 0, queuedSeconds: 0, bankedMinutes: 30 });
  });

  it('counts what runs now and what waits after it', () => {
    const st = {
      bankedSeconds: 0,
      unlockedUntil: at('16:30'),
      windows: [
        { id: 'a', startsAt: at('15:50'), endsAt: at('16:10') },
        { id: 'b', startsAt: at('16:10'), endsAt: at('16:30') },
        { id: 'c', startsAt: at('17:00'), endsAt: at('17:15') },
      ],
    };
    // Back-to-back windows run as one; the 17:00 one is queued.
    expect(screenState(st, now)).toEqual({ runningSeconds: 1800, queuedSeconds: 900, bankedMinutes: 0 });
  });
});

describe('labels', () => {
  it('names minutes', () => {
    expect([45, 60, 90].map(minutesLabel)).toEqual(['45 min', '1 hr', '1 hr 30 min']);
  });

  it('names each kind of credit', () => {
    expect(creditTitle({ source: 'redemption', rewardTitle: '30 min of iPad' })).toBe('30 min of iPad');
    expect(creditTitle({ source: 'grant', grantedBy: 'Sam' })).toBe('From Sam');
    expect(creditTitle({ source: 'time_request', grantedBy: 'Sam' })).toBe('Sam said yes');
    expect(creditLeft({ seconds: 1800, usedSeconds: 650 })).toBe(19);
  });
});

describe('spendChoices', () => {
  it('offers what the bank holds, and the rest in one tap', () => {
    expect(spendChoices(0)).toEqual([]);
    expect(spendChoices(10)).toEqual([10]);
    expect(spendChoices(30)).toEqual([15, 30]);
    expect(spendChoices(45)).toEqual([15, 30, 45]);
    expect(spendChoices(600)).toEqual([15, 30, 60, 240]);
  });
});

describe('domainsFromText', () => {
  it('splits on lines, commas and spaces', () => {
    expect(domainsFromText('games.example.test\n\n  puzzles.example.test, blocks.example.test ')).toEqual([
      'games.example.test',
      'puzzles.example.test',
      'blocks.example.test',
    ]);
  });
});
