import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { useAttempts } from './client';

describe('useAttempts', () => {
  const key = (p: { header: { 'Idempotency-Key': string } }) => p.header['Idempotency-Key'];

  it('keeps one key per attempt until it is settled', () => {
    const { result } = renderHook(() => useAttempts());
    const a = result.current;
    const first = key(a.params('reward-1'));
    expect(key(a.params('reward-1'))).toBe(first); // a retry
    expect(key(a.params('reward-2'))).not.toBe(first); // another change
    a.settle('reward-1');
    expect(key(a.params('reward-1'))).not.toBe(first); // a new change
  });

  it('keeps the key when the request got no answer', () => {
    const { result } = renderHook(() => useAttempts());
    const a = result.current;
    const first = key(a.params('x'));
    a.settleIfAnswered('x')(undefined, new TypeError('Failed to fetch'));
    expect(key(a.params('x'))).toBe(first);
    a.settleIfAnswered('x')(undefined, { error: { code: 'not_enough_points', message: 'No.' } });
    expect(key(a.params('x'))).not.toBe(first);
  });
});
