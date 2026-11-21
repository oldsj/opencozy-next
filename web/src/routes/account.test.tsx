import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import { AccountPage } from './account';

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
const me = { adult: { id: '0195f2d4-0000-7000-8000-000000000001', email: 'sam@example.test', name: 'Sam' }, household: { id: '0195f2d4-0000-7000-8000-000000000002', name: 'Example', timeZone: 'UTC', version: 1 } };

/** A server with single sign-on as Example ID. unlinkAnswers are DELETE's answers, in turn. */
function serve(oidc: unknown, link: unknown, unlinkAnswers: Response[] = []) {
  let current = link;
  const fetchMock = vi.fn((request: Request) => {
    const key = `${request.method} ${new URL(request.url).pathname}`;
    switch (key) {
      case 'GET /api/auth/me':
        return Promise.resolve(json(200, me));
      case 'GET /api/auth/methods':
        return Promise.resolve(json(200, { passkeys: false, oidc }));
      case 'GET /api/auth/oidc/link':
        return Promise.resolve(json(200, { link: current }));
      case 'POST /api/auth/confirm':
        return Promise.resolve(new Response(null, { status: 204 }));
      case 'DELETE /api/auth/oidc/link': {
        const answer = unlinkAnswers.shift() ?? new Response(null, { status: 204 });
        if (answer.status === 204) current = null;
        return Promise.resolve(answer);
      }
    }
    return Promise.resolve(json(404, { error: { code: 'not_found', message: 'Not found.' } }));
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false }, queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <AccountPage />
    </QueryClientProvider>,
  );
}

beforeAll(() => {
  // jsdom has <dialog> but not its modal methods.
  HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement) {
    this.setAttribute('open', '');
  };
  HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement) {
    this.removeAttribute('open');
  };
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('AccountPage', () => {
  it('shows the link and unlinks it, asking for the password when the sign-in is stale', async () => {
    const fetchMock = serve({ name: 'Example ID' }, { email: 'sam@example.test', linkedAt: '2026-09-04T10:00:00Z' }, [
      json(403, { error: { code: 'reauthentication_required', message: 'Confirm your sign-in.' } }),
    ]);
    renderPage();

    expect(await screen.findByText('Linked to Example ID')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Unlink Example ID' }));
    // The dialog's confirm button has the same name as the row's; it comes last.
    const confirm = (await screen.findAllByRole('button', { name: 'Unlink Example ID' })).at(-1);
    if (!confirm) throw new Error('no confirm button');
    await userEvent.click(confirm);
    // Stale: the password dialog asks, then the unlink runs again.
    await userEvent.type(await screen.findByLabelText('Password'), 'a long enough password');
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }));
    expect(await screen.findByText(/Not linked\. Choose “Sign in with Example ID”/)).toBeInTheDocument();
    const deletes = fetchMock.mock.calls.filter(([r]) => r.method === 'DELETE');
    expect(deletes).toHaveLength(2);
  });

  it('has no single sign-on section when it isn’t set up', async () => {
    serve(null, null);
    renderPage();
    expect(await screen.findByText('Signed in as sam@example.test.')).toBeInTheDocument();
    expect(screen.queryByText('Single sign-on')).toBeNull();
  });
});
