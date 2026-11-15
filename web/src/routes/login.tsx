import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import type { SubmitEvent } from 'react';

import { Button, Input, Notice } from '@app/design';

import { $api, errorMessage, meQuery } from '../api/client';
import { field } from '../api/form';

// Adults are added with `app bootstrap-owner` and `app add-adult`; there is no sign-up.
export function LoginPage({ ended = false }: { ended?: boolean }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const login = $api.useMutation('post', '/auth/login');

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
    <section className="mx-auto max-w-sm">
      <h1 className="mb-6 text-2xl font-semibold">Sign in</h1>
      {ended && (
        <div className="mb-6">
          <Notice tone="info">Your sign-in ended. Sign in again to carry on.</Notice>
        </div>
      )}
      <form onSubmit={onSubmit} className="space-y-4">
        <Input label="Email" name="email" type="email" required autoComplete="email" />
        <Input label="Password" name="password" type="password" required maxLength={256} autoComplete="current-password" />
        {login.isError && <Notice tone="error">{errorMessage(login.error)}</Notice>}
        <Button type="submit" variant="primary" fullWidth disabled={login.isPending}>
          Sign in
        </Button>
      </form>
    </section>
  );
}
