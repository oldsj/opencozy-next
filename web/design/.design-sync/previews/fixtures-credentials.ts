import type { ProfileColor } from '../../src';

/** Slice 6, typed to api/src/credentials.yaml and children.yaml (fictional data). */
export type CredentialKind = 'service' | 'assistant';

/** GET /api/credentials: live credentials only; a revoked one stops working at once and leaves the list. */
export interface Credential {
  id: string;
  kind: CredentialKind;
  /** Up to 80 characters. */
  name: string;
  /** 1 to 32 scopes. */
  scopes: string[];
  allChildren: boolean;
  childIds: string[];
  createdAt: string;
  expiresAt?: string | undefined;
}

/** POST /api/credentials: needs a sign-in within 10 minutes; the secret is in this response only. */
export interface CreateCredentialResponse {
  credential: Credential;
  secret: string;
}

/** The scopes the API declares today, with a plain description for adults. */
export const scopeLabels: Record<string, string> = {
  'household:read': 'See the household’s name and settings',
  'children:read': 'See the children, their points and their day',
};

export const kindLabels: Record<CredentialKind, { label: string; detail: string }> = {
  service: { label: 'Service', detail: 'A home server or automation, such as a router or a smart-home hub.' },
  assistant: { label: 'Assistant', detail: 'An AI assistant that answers questions about the family’s day.' },
};

export interface ChildRow {
  id: string;
  name: string;
  color: ProfileColor;
  archived: boolean;
  version: number;
}

export const children: ChildRow[] = [
  { id: 'c1', name: 'Ada', color: 'sky', archived: false, version: 4 },
  { id: 'c2', name: 'Ezra', color: 'sage', archived: false, version: 2 },
  { id: 'c3', name: 'Noor', color: 'lilac', archived: false, version: 1 },
  { id: 'c4', name: 'Robin', color: 'peach', archived: true, version: 3 },
];

export const credentials: Credential[] = [
  { id: 'k1', kind: 'service', name: 'Home server', scopes: ['household:read', 'children:read'], allChildren: true, childIds: [], createdAt: 'Sep 2' },
  { id: 'k2', kind: 'assistant', name: 'Kitchen assistant', scopes: ['children:read'], allChildren: false, childIds: ['c1', 'c2'], createdAt: 'Sep 9', expiresAt: 'Dec 9' },
];

export const issued: CreateCredentialResponse = {
  credential: { id: 'k3', kind: 'service', name: 'Router', scopes: ['children:read'], allChildren: true, childIds: [], createdAt: 'Today' },
  secret: 'ocs_7Kq2XmP4vN8rT3wY6zB9cD5fH1jL0aE',
};
