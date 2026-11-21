import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { errorMessage } from '../api/client';

// The router is mocked: these tests cover the form, not navigation.
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => vi.fn() }));
const { LoginPage, browser, isSsoError, ssoErrorCodes, ssoErrorText } = await import('./login');
type LoginProps = Parameters<typeof LoginPage>[0];

function renderPage(props: LoginProps = {}) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false }, queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <LoginPage {...props} />
    </QueryClientProvider>,
  );
}

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

/** A server whose sign-in methods are methods; other paths answer from routes. */
function serve(methods: unknown, routes: Record<string, () => Response> = {}) {
  const fetchMock = vi.fn((request: Request) => {
    const path = new URL(request.url).pathname;
    if (path === '/api/auth/methods') return Promise.resolve(json(200, methods));
    const route = routes[`${request.method} ${path}`];
    return Promise.resolve(route ? route() : json(404, { error: { code: 'not_found', message: 'Not found.' } }));
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}
const requests = (fetchMock: ReturnType<typeof serve>) => fetchMock.mock.calls.map(([r]) => `${r.method} ${new URL(r.url).pathname}`);

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('LoginPage', () => {
  it('shows the server error message on failed sign-in', async () => {
    const fetchMock = serve({ passkeys: false, oidc: null }, {
      'POST /api/auth/login': () => json(401, { error: { code: 'invalid_credentials', message: 'Wrong email or password.' } }),
    });
    renderPage();

    await userEvent.type(screen.getByLabelText('Email'), 'ada@example.com');
    await userEvent.type(screen.getByLabelText('Password'), 'a long enough password');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Wrong email or password.');
    expect(requests(fetchMock)).toContain('POST /api/auth/login');
  });

  it('offers single sign-on only when it is set up, and goes to the provider', async () => {
    const go = vi.spyOn(browser, 'go').mockImplementation(() => undefined);
    const fetchMock = serve({ passkeys: false, oidc: { name: 'Example ID' } }, {
      'POST /api/auth/oidc/start': () => json(200, { url: 'https://id.example.test/authorize?state=x' }),
    });
    renderPage();

    await userEvent.click(await screen.findByRole('button', { name: 'Sign in with Example ID' }));
    await vi.waitFor(() => { expect(go).toHaveBeenCalledWith('https://id.example.test/authorize?state=x'); });
    expect(requests(fetchMock)).toContain('POST /api/auth/oidc/start');
    expect(screen.getByRole('button', { name: 'Sign in with password' })).toBeInTheDocument();
  });

  it('never leaves for anything but a web address', async () => {
    const go = vi.spyOn(browser, 'go').mockImplementation(() => undefined);
    serve({ passkeys: false, oidc: { name: 'Example ID' } }, {
      'POST /api/auth/oidc/start': () => json(200, { url: 'javascript:alert(1)' }),
    });
    renderPage();
    await userEvent.click(await screen.findByRole('button', { name: 'Sign in with Example ID' }));
    await new Promise((r) => setTimeout(r, 50));
    expect(go).not.toHaveBeenCalled();
  });

  it('has no single sign-on button without it', async () => {
    serve({ passkeys: false, oidc: null });
    renderPage();
    expect(await screen.findByRole('button', { name: 'Sign in' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Sign in with/ })).toBeNull();
  });

  it('says why single sign-on sent the adult back, naming the provider', async () => {
    serve({ passkeys: false, oidc: { name: 'Example ID' } });
    renderPage({ ssoError: 'oidc_unverified' });
    expect(await screen.findByText('Example ID hasn’t confirmed that email yet, so it can’t be used to sign in here.')).toBeInTheDocument();
  });
});

describe('ssoErrorText', () => {
  it('words every code the callback sends', () => {
    for (const code of ssoErrorCodes) expect(ssoErrorText(code, 'Example ID')).not.toBe('');
    expect(isSsoError('oidc_no_adult')).toBe(true);
    expect(isSsoError('<script>')).toBe(false);
  });
});

describe('errorMessage', () => {
  it('says when the request got no answer', () => {
    expect(errorMessage(undefined)).toMatch(/couldn’t reach the server/i);
    expect(errorMessage(new TypeError('Failed to fetch'))).toMatch(/couldn’t reach the server/i);
    expect(errorMessage({ error: { code: 'x', message: 'Nope.' } })).toBe('Nope.');
  });
});
