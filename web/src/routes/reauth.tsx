import { useState } from 'react';

import { ConfirmDialog, Input } from '@app/design';

import { $api, errorCode, errorMessage } from '../api/client';

// Pairing a display or a device, and issuing credentials, need a sign-in within the last 10
// minutes. When the server asks for the password again, this asks for it, confirms the sign-in,
// and runs the change again (with the same Idempotency-Key, so it still happens once).
export function useReauth() {
  const [again, setAgain] = useState<(() => void) | null>(null);
  const [password, setPassword] = useState('');
  const confirm = $api.useMutation('post', '/auth/confirm');
  const close = () => {
    setAgain(null);
    setPassword('');
    confirm.reset();
  };

  /** True when the error asks for the password; the change is run again once it is confirmed. */
  const ask = (error: unknown, retry: () => void): boolean => {
    if (errorCode(error) !== 'reauthentication_required') return false;
    setAgain(() => retry);
    return true;
  };

  const dialog = (
    <ConfirmDialog
      open={again !== null}
      title="Enter your password again"
      description="This needs a recent sign-in."
      confirmLabel="Continue"
      pending={confirm.isPending}
      error={confirm.error ? errorMessage(confirm.error) : undefined}
      onCancel={close}
      onConfirm={() => {
        confirm.mutate({ body: { password } }, {
          onSuccess: () => {
            const retry = again;
            close();
            retry?.();
          },
        });
      }}
    >
      <Input
        label="Password"
        type="password"
        autoComplete="current-password"
        maxLength={256}
        value={password}
        onChange={(e) => { setPassword(e.target.value); }}
      />
    </ConfirmDialog>
  );

  return { ask, dialog, needsPassword: (error: unknown) => errorCode(error) === 'reauthentication_required' };
}
