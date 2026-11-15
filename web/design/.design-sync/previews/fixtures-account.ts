/**
 * Sign-in and account (slices 10a, 10b, 11), fictional data. Passkey follows api/src/passkeys.yaml
 * (#26). Single sign-on and phone sessions are shaped like the old app's docs; the builders adjust.
 */

/** GET /api/auth/passkeys → {passkeys}, oldest first. */
export interface Passkey {
  id: string;
  /** 1 to 80 characters; renaming needs a PATCH that the contract doesn't have yet. */
  name: string;
  createdAt: string;
  lastUsedAt?: string | undefined;
  /** Synced to a password manager or cloud keychain, so it survives losing the device. */
  backedUp: boolean;
}

/**
 * How adults can sign in here. Single sign-on is set up by whoever runs OpenCozy (issuer, client and
 * an optional button name); null when it isn't.
 */
export interface SignInMethods {
  oidc: { name: string } | null;
}

/**
 * Single sign-on never creates anyone: the provider's account links on first use to the adult whose
 * verified email matches. Unlinking leaves password and passkeys working.
 */
export interface OidcLink {
  providerName: string;
  email: string;
  linkedAt: string;
}

/** A parent phone's app session: a 90-day bearer, listed so it can be signed out from the web. */
export interface PhoneSession {
  id: string;
  deviceName: string;
  createdAt: string;
  lastUsedAt: string;
  expiresAt: string;
  pushEnabled: boolean;
}

export const me = { name: 'Sam', email: 'sam@example.test' };

export const passkeys: Passkey[] = [
  { id: 'pk1', name: 'Sam’s iPhone', createdAt: 'Aug 30', lastUsedAt: 'Today', backedUp: true },
  { id: 'pk2', name: 'Security key', createdAt: 'Sep 2', backedUp: false },
];

export const methods: SignInMethods = { oidc: { name: 'Pocket ID' } };

export const oidcLink: OidcLink = { providerName: 'Pocket ID', email: 'sam@example.test', linkedAt: 'Sep 4' };

export const phoneSessions: PhoneSession[] = [
  { id: 'ps1', deviceName: 'Sam’s iPhone', createdAt: 'Sep 3', lastUsedAt: '5 minutes ago', expiresAt: 'Dec 2', pushEnabled: true },
  { id: 'ps2', deviceName: 'Old iPhone', createdAt: 'Jun 18', lastUsedAt: 'Aug 1', expiresAt: 'Sep 16', pushEnabled: false },
];

/** Errors in words, keyed by the API's codes. */
export const accountErrorText = {
  invalid_credentials: 'That email and password don’t match. Check them and try again.',
  passkey_not_recognized: 'This passkey isn’t set up here. Sign in with your password, then add it under Account.',
  passkey_cancelled: 'No passkey was used. Try again, or use your password.',
  oidc_no_adult: 'No adult here uses that email. Sign in another way, or ask whoever set up OpenCozy.',
  oidc_unverified: 'Pocket ID hasn’t confirmed that email yet, so it can’t be used to sign in here.',
  too_many_attempts: 'Too many tries. Wait 15 minutes, then try again.',
  wrong_password: 'That password isn’t right.',
} as const;
