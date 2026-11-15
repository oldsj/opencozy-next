// The only way the app talks to the server: a typed client generated from api/openapi.yaml.
// Run `make gen` after changing the contract; type errors then show every call site to fix.
import createFetchClient from 'openapi-fetch';
import createQueryClient from 'openapi-react-query';
import { queryOptions } from '@tanstack/react-query';
import { useMemo, useRef } from 'react';

import type { components, paths } from './schema.gen';

export type Me = components['schemas']['Me'];
export type Child = components['schemas']['Child'];
export type ApiError = components['schemas']['Error'];

// Same origin as the page. An absolute URL, because Node's Request (under Vitest) rejects relative
// ones. fetch is looked up per call, not captured once, so tests can stub it.
export const client = createFetchClient<paths>({
  baseUrl: new URL('/api', window.location.origin).href,
  fetch: (req) => globalThis.fetch(req),
});
export const $api = createQueryClient(client);

/** Whether an error is the server's answer (an error body), rather than a request that never got one. */
export function isAnswer(error: unknown): error is ApiError {
  return typeof error === 'object' && error !== null && 'error' in error;
}

/** The error's stable code, if the server answered. */
export function errorCode(error: unknown): string | undefined {
  return isAnswer(error) ? error.error.code : undefined;
}

/** The user's message from an API error body, or one for a request that got no answer. */
export function errorMessage(error: unknown): string {
  if (!isAnswer(error)) return 'Couldn’t reach the server. Check the connection and try again.';
  return error.error.message;
}

// Signed-out handling: a 401 from a signed-in page means the session ended (expired, revoked,
// signed out elsewhere). The router registers what to do; the wall display and sign-in itself
// handle their own 401s.
let signedOut: (() => void) | undefined;
export function onSignedOut(handler: () => void) {
  signedOut = handler;
}
client.use({
  onResponse({ request, response }) {
    const path = new URL(request.url).pathname;
    if (response.status === 401 && !path.startsWith('/api/auth/') && !window.location.pathname.startsWith('/display')) {
      signedOut?.();
    }
    return undefined;
  },
});

/** The signed-in adult and their household, or null when signed out. Shared by the router guard and the UI. */
export const meQuery = queryOptions({
  queryKey: ['get', '/auth/me'],
  queryFn: async (): Promise<Me | null> => {
    const { data, response } = await client.GET('/auth/me');
    if (response.status === 401) return null;
    if (!data) throw new Error(`GET /auth/me: ${String(response.status)}`);
    return data;
  },
  staleTime: 60_000,
});

export type Display = components['schemas']['Display'];
export type DisplayScope = components['schemas']['DisplayScope'];

// A fresh Idempotency-Key for one change. Reuse the same key when retrying that change.
export function idempotencyKey(): { header: { 'Idempotency-Key': string } } {
  return { header: { 'Idempotency-Key': crypto.randomUUID() } };
}

/**
 * One Idempotency-Key per change, kept until the server gives a definite answer. If a response
 * is lost after the server made the change, the retry sends the same key and the server replays
 * its answer instead of making the change again. Name an attempt by what it changes (for a create,
 * its body), and settle it once answered or abandoned.
 */
const attemptTTL = 60 * 60_000; // a pending key older than this is a new change

// Keys kept in sessionStorage (when `store` is given) outlive a reload or a closed sheet: a tap
// whose answer was lost, then a reload, still sends the same key, so the server replays the
// outcome instead of doing it twice. Storage can be missing or throw (private mode, a full
// quota); then the keys live in memory only.
function stored(store: string | undefined) {
  const name = (attempt: string) => `opencozy:attempt:${store ?? ''}:${attempt}`;
  return {
    get(attempt: string): string | undefined {
      if (!store) return undefined;
      try {
        const raw = sessionStorage.getItem(name(attempt));
        if (!raw) return undefined;
        const { key, at } = JSON.parse(raw) as { key?: unknown; at?: unknown };
        if (typeof key === 'string' && typeof at === 'number' && Date.now() - at < attemptTTL) return key;
        sessionStorage.removeItem(name(attempt));
      } catch {
        // unreadable: memory only
      }
      return undefined;
    },
    set(attempt: string, key: string) {
      if (!store) return;
      try {
        sessionStorage.setItem(name(attempt), JSON.stringify({ key, at: Date.now() }));
      } catch {
        // full or blocked: memory only
      }
    },
    delete(attempt: string) {
      if (!store) return;
      try {
        sessionStorage.removeItem(name(attempt));
      } catch {
        // blocked: nothing to remove
      }
    },
  };
}

export function useAttempts(store?: string) {
  const keys = useRef(new Map<string, string>());
  return useMemo(() => {
    const saved = stored(store);
    const forget = (attempt: string) => {
      keys.current.delete(attempt);
      saved.delete(attempt);
    };
    return {
      params(attempt: string): { header: { 'Idempotency-Key': string } } {
        let key = keys.current.get(attempt) ?? saved.get(attempt);
        if (!key) {
          key = crypto.randomUUID();
          saved.set(attempt, key);
        }
        keys.current.set(attempt, key);
        return { header: { 'Idempotency-Key': key } };
      },
      /** The answer came (or the user gave up): a later try is a new change. */
      settle: forget,
      /** For a mutation's onSettled: settle unless the request got no answer. */
      settleIfAnswered(attempt: string) {
        return (_data: unknown, error: unknown) => {
          if (!error || isAnswer(error)) forget(attempt);
        };
      },
    };
  }, [store]);
}

export type MealWeek = components['schemas']['MealWeek'];
export type Meal = components['schemas']['Meal'];
export type MealSlot = components['schemas']['MealSlot'];
export type Recipe = components['schemas']['Recipe'];
export type RecipeSummary = components['schemas']['RecipeSummary'];
export type Today = components['schemas']['Today'];
export type TodayGroup = components['schemas']['TodayGroup'];
export type TodayItem = components['schemas']['TodayItem'];
export type Household = components['schemas']['Household'];
export type InboxItem = components['schemas']['InboxItem'];
export type HistoryEntry = components['schemas']['HistoryEntry'];
