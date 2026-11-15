// @dsCard group="Overlays"
import { Button, ConfirmDialog, Input } from '../../src';

import { accountErrorText } from './fixtures-account';

export function Archive() {
  return (
    <ConfirmDialog
      open
      tone="danger"
      title="Archive Tablet time?"
      description="Children won’t see it any more. History keeps every time it was chosen."
      confirmLabel="Archive"
      onConfirm={() => undefined}
      onCancel={() => undefined}
    />
  );
}

/**
 * "Confirm it’s you" before a sensitive change (pairing a device, adding a key or a passkey) when the
 * last sign-in is more than 10 minutes old. The passkey is the quick way; the password always works.
 * Confirming carries straight on with what the adult was doing.
 */
function Reauth({ waiting = false, error }: { waiting?: boolean; error?: string | undefined }) {
  return (
    <ConfirmDialog
      open
      title="Confirm it’s you"
      description="You signed in a while ago. Confirm it’s you to pair Ada’s iPad."
      confirmLabel="Confirm with password"
      pending={waiting}
      error={error}
      onConfirm={() => undefined}
      onCancel={() => undefined}
    >
      <div className="grid gap-4">
        <Button variant="primary" fullWidth disabled={waiting}>
          {waiting ? 'Waiting for your passkey…' : 'Use your passkey'}
        </Button>
        <Input label="Or your password" type="password" autoComplete="current-password" maxLength={256} />
      </div>
    </ConfirmDialog>
  );
}

export function ConfirmItsYou() {
  return <Reauth />;
}

export function ConfirmItsYouWaiting() {
  return <Reauth waiting />;
}

export function ConfirmItsYouWrongPassword() {
  return <Reauth error={accountErrorText.wrong_password} />;
}
