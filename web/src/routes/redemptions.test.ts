import { describe, expect, it } from 'vitest';

import { refundFor } from './redemptions';

describe('refundFor', () => {
  it('gives all of an approved reward back', () => {
    expect(refundFor({ status: 'approved', cost: 7 })).toBe(7);
  });
  it('gives screen time’s unused share back, rounded down', () => {
    expect(refundFor({ status: 'fulfilled', cost: 10, minutes: 30, usedMinutes: 10 })).toBe(6);
    expect(refundFor({ status: 'fulfilled', cost: 10, minutes: 30 })).toBe(10);
    expect(refundFor({ status: 'fulfilled', cost: 10, minutes: 30, usedMinutes: 29 })).toBe(0);
    expect(refundFor({ status: 'fulfilled', cost: 10, minutes: 30, usedMinutes: 45 })).toBe(0);
  });
  it('gives nothing back for a given item or a settled one', () => {
    expect(refundFor({ status: 'fulfilled', cost: 5 })).toBe(0);
    expect(refundFor({ status: 'cancelled', cost: 5 })).toBe(0);
    expect(refundFor({ status: 'rejected', cost: 5 })).toBe(0);
  });
});
