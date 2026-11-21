import { describe, expect, it, vi } from 'vitest';

import { screenTimeChanged } from './native';

describe('screenTimeChanged', () => {
  it('tells the iPad app when its bridge is there', () => {
    const postMessage = vi.fn();
    screenTimeChanged({ webkit: { messageHandlers: { opencozy: { postMessage } } } });
    expect(postMessage).toHaveBeenCalledWith({ type: 'screenTimeChanged' });
  });

  it('does nothing in a browser', () => {
    expect(() => { screenTimeChanged({}); }).not.toThrow();
    expect(() => { screenTimeChanged({ webkit: { messageHandlers: {} } }); }).not.toThrow();
  });

  it('survives a handler that throws', () => {
    const postMessage = vi.fn(() => { throw new Error('gone'); });
    expect(() => { screenTimeChanged({ webkit: { messageHandlers: { opencozy: { postMessage } } } }); }).not.toThrow();
  });
});
