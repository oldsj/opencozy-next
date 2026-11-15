import type { ReadingStatus } from '../../src';

/**
 * Slice 15 (Home Assistant), shaped like the old app's (fictional data). OpenCozy only reads states
 * of entities an adult shares; it never calls services. The token is write-only. Displays read a
 * server-side cache; a value is current for 5 minutes, then shown as stale with its age.
 */
export interface HomeConnection {
  /** Base URL; https, or http on a LAN subnet the operator allowed. */
  url: string;
  status: 'unverified' | 'ok' | 'error';
  lastCheckedAt: string | null;
  errorMessage: string | null;
}

/** Only these attributes ever reach a client (the old app's allowlist). */
export interface EntityAttributes {
  friendly_name?: string | undefined;
  unit_of_measurement?: string | undefined;
  device_class?: string | undefined;
}

export type HomeEntity =
  | { entityId: string; status: 'ok'; state: string; attributes: EntityAttributes; fetchedAt: string }
  | { entityId: string; status: 'stale'; state: string; attributes: EntityAttributes; asOf: string }
  | { entityId: string; status: 'error'; fetchedAt: string | null }
  | { entityId: string; status: 'pending' };

export const connection: HomeConnection = {
  url: 'http://homeassistant.example.test:8123',
  status: 'ok',
  lastCheckedAt: '4:10 PM',
  errorMessage: null,
};

export const entities: HomeEntity[] = [
  { entityId: 'sensor.kitchen_temperature', status: 'ok', state: '71', attributes: { friendly_name: 'Kitchen', unit_of_measurement: '°F', device_class: 'temperature' }, fetchedAt: '4:10 PM' },
  { entityId: 'sensor.outside_temperature', status: 'ok', state: '48', attributes: { friendly_name: 'Outside', unit_of_measurement: '°F', device_class: 'temperature' }, fetchedAt: '4:10 PM' },
  { entityId: 'binary_sensor.back_door', status: 'ok', state: 'on', attributes: { friendly_name: 'Back door', device_class: 'door' }, fetchedAt: '4:10 PM' },
  { entityId: 'lock.front_door', status: 'ok', state: 'locked', attributes: { friendly_name: 'Front door' }, fetchedAt: '4:10 PM' },
  { entityId: 'sensor.washing_machine', status: 'stale', state: 'Running', attributes: { friendly_name: 'Washing machine' }, asOf: '3:52 PM' },
  { entityId: 'sensor.garden_rain', status: 'error', fetchedAt: '4:10 PM' },
  { entityId: 'sensor.solar_power', status: 'pending' },
];

export const homeErrorText = {
  connection_failed: 'OpenCozy couldn’t reach Home Assistant at that address. Check the URL and that it’s on the network.',
  token_rejected: 'Home Assistant didn’t accept that token. Create a new long-lived access token and paste it here.',
  invalid_entity: 'Enter an entity ID such as sensor.kitchen_temperature.',
  entity_exists: 'That entity is already shared.',
} as const;

const words: Record<string, Record<string, string>> = {
  door: { on: 'Open', off: 'Closed' },
  window: { on: 'Open', off: 'Closed' },
  garage_door: { on: 'Open', off: 'Closed' },
  opening: { on: 'Open', off: 'Closed' },
  motion: { on: 'Movement', off: 'Still' },
  moisture: { on: 'Wet', off: 'Dry' },
  lock: { locked: 'Locked', unlocked: 'Unlocked', jammed: 'Jammed', locking: 'Locking', unlocking: 'Unlocking' },
};

/** A state needing a glance: something open, unlocked, wet or jammed. */
const attention = new Set(['Open', 'Unlocked', 'Wet', 'Jammed']);

/** An entity as Reading props: its name, the state in words, and how current it is. */
export interface ReadingText {
  label: string;
  value?: string | undefined;
  unit?: string | undefined;
  status: ReadingStatus;
  note?: string | undefined;
  tone?: 'neutral' | 'attention' | undefined;
}

export function readingFor(e: HomeEntity): ReadingText {
  // No friendly name (or nothing read yet): the entity's object id, as words. "garden_rain" → "Garden rain".
  const spaced = e.entityId.split('.')[1]?.replaceAll('_', ' ') ?? e.entityId;
  const fallback = spaced.charAt(0).toUpperCase() + spaced.slice(1);
  if (e.status === 'error') return { label: fallback, status: 'error', note: 'Home Assistant didn’t answer' };
  if (e.status === 'pending') return { label: fallback, status: 'pending' };
  const kind = e.entityId.startsWith('lock.') ? 'lock' : (e.attributes.device_class ?? '');
  const value = words[kind]?.[e.state] ?? e.state;
  return {
    label: e.attributes.friendly_name ?? fallback,
    value,
    unit: e.attributes.unit_of_measurement,
    status: e.status === 'ok' ? 'current' : 'stale',
    note: e.status === 'stale' ? `As of ${e.asOf}` : undefined,
    tone: attention.has(value) ? 'attention' : 'neutral',
  };
}
