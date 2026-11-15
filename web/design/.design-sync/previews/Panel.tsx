// @dsCard group="Layout"
import { Button, Input, Notice, Panel } from '../../src';

import { accountErrorText, methods } from './fixtures-account';

export function Default() {
  return (
    <Panel heading="Today’s responsibilities" description="Choose a person to see what is waiting.">
      <p>Three chores are ready for the family.</p>
    </Panel>
  );
}

export function Tones() {
  return (
    <div className="grid gap-4">
      <Panel tone="positive" heading="Chore confirmed" description="Ezra put away the board games.">
        <p>10 points added to Ezra’s balance.</p>
      </Panel>
      <Panel tone="attention" heading="Close to a reward" description="Ada is 8 points from Movie night." />
      <Panel tone="error" heading="Couldn’t save the chore" description="Check the connection and try again." />
      <Panel tone="info" heading="Meals are coming later" description="Meal planning isn’t ready yet." />
    </div>
  );
}

type SignInState = 'ready' | 'error' | 'passkey' | 'oidcError' | 'ended';

/**
 * The sign-in page (/login). A passkey is the quick way; the password always works; single sign-on
 * shows only when the household's server has it set up. There is no sign-up: adults are added.
 */
function SignIn({ state = 'ready', oidc = methods.oidc }: { state?: SignInState; oidc?: { name: string } | null }) {
  const waiting = state === 'passkey';
  return (
    <div className="mx-auto grid max-w-sm gap-6">
      <Panel heading="Sign in" description="For adults in the household.">
        <div className="grid gap-4">
          {state === 'ended' && <Notice tone="info">Your sign-in ended. Sign in again to carry on.</Notice>}
          {state === 'oidcError' && <Notice tone="error">{accountErrorText.oidc_no_adult}</Notice>}
          <Button variant="primary" fullWidth disabled={waiting}>
            {waiting ? 'Waiting for your passkey…' : 'Sign in with a passkey'}
          </Button>
          {oidc && (
            <Button fullWidth disabled={waiting}>
              Sign in with {oidc.name}
            </Button>
          )}
          <form className="grid gap-4 border-t border-border pt-4" onSubmit={(e) => { e.preventDefault(); }}>
            <Input label="Email" name="email" type="email" autoComplete="username webauthn" defaultValue={state === 'error' ? 'sam@example.test' : undefined} required />
            <Input label="Password" name="password" type="password" autoComplete="current-password" maxLength={256} required />
            {state === 'error' && <Notice tone="error">{accountErrorText.invalid_credentials}</Notice>}
            <Button type="submit" fullWidth disabled={waiting}>
              Sign in with password
            </Button>
          </form>
        </div>
      </Panel>
    </div>
  );
}

export function SignInPage() {
  return <SignIn />;
}

/** Without single sign-on set up, the button isn't there at all. */
export function SignInNoSso() {
  return <SignIn oidc={null} />;
}

/** The browser's passkey prompt is open; the page waits and nothing else can be pressed. */
export function SignInPasskeyWaiting() {
  return <SignIn state="passkey" />;
}

export function SignInWrongPassword() {
  return <SignIn state="error" />;
}

/** Back from the provider with an email no adult here uses: nobody is created. */
export function SignInSsoNoMatch() {
  return <SignIn state="oidcError" />;
}

export function SignInEnded() {
  return <SignIn state="ended" />;
}
