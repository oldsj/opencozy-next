import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useEffect, useState, type SubmitEvent } from 'react';

import { Button, Input, Notice, Panel } from '@app/design';

import { $api, errorMessage, meQuery } from '../api/client';
import { field } from '../api/form';

/** The codes /api/auth/callback/oidc sends the browser back with (api/src/oidc.yaml). */
export const ssoErrorCodes = ['oidc_failed', 'oidc_no_adult', 'oidc_unverified', 'oidc_linked_other', 'oidc_off', 'kid_device'] as const;
export type SsoError = (typeof ssoErrorCodes)[number];

export function isSsoError(code: unknown): code is SsoError {
  return typeof code === 'string' && (ssoErrorCodes as readonly string[]).includes(code);
}

/** A single sign-on error in words; provider is the configured button name. */
export function ssoErrorText(code: SsoError, provider: string): string {
  switch (code) {
    case 'oidc_no_adult':
      return 'No adult here uses that email. Sign in another way, or ask whoever set up OpenCozy.';
    case 'oidc_unverified':
      return `${provider} hasn’t confirmed that email yet, so it can’t be used to sign in here.`;
    case 'oidc_linked_other':
      return `You’re linked to a different ${provider} account. Sign in another way, then unlink it under Account.`;
    case 'oidc_off':
      return 'Single sign-on isn’t set up here. Sign in with your password.';
    case 'kid_device':
      return 'This is a child’s device. Sign in on your own phone or computer.';
    case 'oidc_failed':
      return `Signing in with ${provider} didn’t finish. Try again, or sign in another way.`;
  }
}

/** Leaving for the provider's sign-in page; a seam for tests, which can't navigate. */
export const browser = {
  go(url: string) {
    window.location.assign(url);
  },
};

// Adults are added with `app bootstrap-owner` and `app add-adult`; there is no sign-up. Single
// sign-on shows only when the server has it set up.
export function LoginPage({ ended = false, ssoError }: { ended?: boolean; ssoError?: SsoError | undefined }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const login = $api.useMutation('post', '/auth/login');
  const methods = $api.useQuery('get', '/auth/methods');
  const sso = methods.data?.oidc ?? null;
  // Leaving for the provider: from the start's answer until the page is gone.
  const [leaving, setLeaving] = useState(false);
  const [badURL, setBadURL] = useState(false);
  const start = $api.useMutation('post', '/auth/oidc/start', {
    onSuccess: ({ url }) => {
      // Only ever a web address: the provider's, as the server built it.
      if (!/^https?:\/\//.test(url)) {
        setBadURL(true);
        return;
      }
      setLeaving(true);
      browser.go(url);
    },
  });
  // Back from the provider's page, a browser may restore this page as it was (the back/forward
  // cache): make both ways of signing in usable again.
  useEffect(() => {
    const restored = (e: PageTransitionEvent) => {
      if (e.persisted) setLeaving(false);
    };
    window.addEventListener('pageshow', restored);
    return () => { window.removeEventListener('pageshow', restored); };
  }, []);
  const ssoBusy = start.isPending || leaving;

  function onSubmit(e: SubmitEvent<HTMLFormElement>) {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    login.mutate(
      { body: { email: field(form, 'email'), password: field(form, 'password') } },
      {
        onSuccess: (me) => {
          queryClient.setQueryData(meQuery.queryKey, me);
          void navigate({ to: '/' });
        },
      },
    );
  }

  return (
    <div className="mx-auto grid max-w-sm gap-6">
      <Panel heading="Sign in" description="For adults in the household.">
        <div className="grid gap-4">
          {ended && <Notice tone="info">Your sign-in ended. Sign in again to carry on.</Notice>}
          {ssoError && <Notice tone="error">{ssoErrorText(ssoError, sso?.name ?? 'single sign-on')}</Notice>}
          {sso && (
            <>
              <Button variant="primary" fullWidth disabled={ssoBusy || login.isPending} onClick={() => { setBadURL(false); start.mutate({}); }}>
                Sign in with {sso.name}
              </Button>
              {start.isError && <Notice tone="error">{errorMessage(start.error)}</Notice>}
              {badURL && <Notice tone="error">{ssoErrorText('oidc_failed', sso.name)}</Notice>}
            </>
          )}
          <form onSubmit={onSubmit} className={sso ? 'grid gap-4 border-t border-border pt-4' : 'grid gap-4'}>
            <Input label="Email" name="email" type="email" required autoComplete="email" />
            <Input label="Password" name="password" type="password" required maxLength={256} autoComplete="current-password" />
            {login.isError && <Notice tone="error">{errorMessage(login.error)}</Notice>}
            <Button type="submit" variant={sso ? 'secondary' : 'primary'} fullWidth disabled={login.isPending || start.isPending}>
              {sso ? 'Sign in with password' : 'Sign in'}
            </Button>
          </form>
        </div>
      </Panel>
    </div>
  );
}
