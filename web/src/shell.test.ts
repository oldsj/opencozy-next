import { describe, expect, it } from 'vitest';

import { manageGroups } from './shell';

const current = (path: string, sso = false) =>
  manageGroups(path, undefined, sso)
    .flatMap((g) => g.items)
    .filter((i) => i.current)
    .map((i) => i.label);

describe('manageGroups', () => {
  it('marks the section a page belongs to', () => {
    expect(current('/')).toEqual(['Overview']);
    expect(current('/chores')).toEqual(['Chores']);
    expect(current('/children/0190a000-0000-7000-8000-0000000000c1/points')).toEqual(['Children']);
    expect(current('/redemptions')).toEqual(['Rewards']);
    expect(current('/calendars')).toEqual(['Calendar']);
    expect(current('/calendar/events/abc')).toEqual(['Calendar']);
    expect(current('/recipes/new')).toEqual(['Recipes']);
    expect(current('/login')).toEqual([]);
  });

  it('counts the inbox, and shows no badge when nothing waits', () => {
    const inbox = (n: number) => manageGroups('/', n, false)[0]?.items.find((i) => i.href === '/inbox');
    expect(inbox(3)?.badge).toBe(3);
    expect(inbox(0)?.badge).toBeUndefined();
  });

  it('lists Account only with single sign-on', () => {
    const labels = (sso: boolean) => manageGroups('/', 0, sso).flatMap((g) => g.items.map((i) => i.label));
    expect(labels(false)).not.toContain('Account');
    expect(labels(true)).toContain('Account');
    expect(current('/account', true)).toEqual(['Account']);
  });
});
