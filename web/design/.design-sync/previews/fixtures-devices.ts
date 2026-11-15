import type { ProfileColor } from '../../src';

/**
 * Slice 7, typed to cozy-rewrite's draft (fictional data). An ADULT makes a pairing offer for a
 * child; the device (native) scans the QR or types the code. Exact paths follow the spec PR.
 */
export interface PairingOffer {
  id: string;
  /** 8 characters shown as XXXX-XXXX; single use. */
  code: string;
  token: string;
  /**
   * Encode into the QR on the client (QrCode takes the module matrix). The app's scanner accepts only
   * `opencozy-pair:v1?origin=<url-encoded origin>&token=<token>` (#36).
   */
  qrPayload: string;
  /** Ten minutes after the offer; single use. */
  expiresAt: string;
}

export interface Device {
  id: string;
  name: string;
  platform: 'ipad' | 'iphone';
  /** Null when the child was archived or the device was never assigned. */
  child: { id: string; name: string; color: ProfileColor } | null;
  pairedAt: string;
  lastSeenAt?: string | undefined;
}

/**
 * GET /api/devices. PATCH {name?, childId?} renames or reassigns; DELETE revokes. The lock message is
 * household-wide: PUT /api/devices/lock-message {lockMessage: string | null} (1-200 characters after
 * trimming; null restores the default).
 */
export interface DevicesResponse {
  devices: Device[];
  /** Null while the default applies. */
  lockMessage: string | null;
  defaultLockMessage: string;
}

export const platformLabels: Record<Device['platform'], string> = { ipad: 'iPad', iphone: 'iPhone' };

export const devices: DevicesResponse = {
  devices: [
    { id: 'd1', name: 'Ada’s iPad', platform: 'ipad', child: { id: 'c1', name: 'Ada', color: 'sky' }, pairedAt: 'Sep 3', lastSeenAt: '2 minutes ago' },
    { id: 'd2', name: 'Ezra’s iPad', platform: 'ipad', child: { id: 'c2', name: 'Ezra', color: 'sage' }, pairedAt: 'Sep 3', lastSeenAt: 'Yesterday' },
    { id: 'd3', name: 'Old iPhone', platform: 'iphone', child: null, pairedAt: 'Jun 18' },
  ],
  lockMessage: null,
  defaultLockMessage: 'Earn time on the chore board, then spend your points in OpenCozy.',
};

export const offer: PairingOffer = {
  id: 'p1',
  code: 'K7QM-2XHT',
  token: 'opaque-token',
  qrPayload: 'opencozy-pair:v1?origin=https%3A%2F%2Fcozy.example.test&token=opaque-token',
  expiresAt: '2026-09-14T16:10:00-04:00',
};

/** The kid view's session (the device's own child). */
export const kid = { id: 'c1', name: 'Ada', color: 'sky' as ProfileColor };
