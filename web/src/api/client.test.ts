import { renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

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

describe('useAttempts with a store', () => {
  const key = (p: { header: { 'Idempotency-Key': string } }) => p.header['Idempotency-Key'];

  it('keeps a pending key across a reload, until it is settled', () => {
    sessionStorage.clear();
    const before = renderHook(() => useAttempts('redeem')).result.current;
    const first = key(before.params('child:reward'));
    const after = renderHook(() => useAttempts('redeem')).result.current; // the page reloaded
    expect(key(after.params('child:reward'))).toBe(first);
    expect(key(renderHook(() => useAttempts('other')).result.current.params('child:reward'))).not.toBe(first);
    after.settle('child:reward');
    expect(key(renderHook(() => useAttempts('redeem')).result.current.params('child:reward'))).not.toBe(first);
  });

  it('forgets a key older than an hour', () => {
    sessionStorage.clear();
    sessionStorage.setItem('opencozy:attempt:redeem:x', JSON.stringify({ key: 'old', at: Date.now() - 2 * 60 * 60_000 }));
    expect(key(renderHook(() => useAttempts('redeem')).result.current.params('x'))).not.toBe('old');
  });

  it('works in memory when storage throws', () => {
    const spy = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('full', 'QuotaExceededError');
    });
    try {
      const a = renderHook(() => useAttempts('redeem')).result.current;
      const first = key(a.params('y'));
      expect(key(a.params('y'))).toBe(first);
    } finally {
      spy.mockRestore();
      set.mockRestore();
    }
  });
});
