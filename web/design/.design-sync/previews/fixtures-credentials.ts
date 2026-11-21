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
  /** Award rules a `learning:award` credential may use; empty otherwise. */
  awardRuleIds: string[];
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
  'learning:award': 'Give points for learning, by your award rules',
  'screentime:read': 'See the children’s iPad time',
  'games:gate': 'Open game websites only while a child’s iPad time runs (a router)',
  'today:read': 'See today’s chores and routines',
  'today:complete': 'Tick off chores and routine steps as you (points land at once)',
  'inbox:read': 'See what waits for your decision',
  'inbox:decide': 'Approve, send back and answer requests for you',
  'points:read': 'See each child’s points history',
  'rewards:read': 'See rewards and what the children redeemed',
  'calendar:read': 'See the family calendar',
  'meals:read': 'See the meal plan',
  'screentime:grant': 'Give and end iPad time as you',
  'tv:read': 'See the TVs, who is watching and TV history',
  'tv:manage': 'Add, edit and remove TVs',
  'tv:control': 'Let TVs online, turn them off, open and close them, as you',
  'tv:gate': 'Let TVs online only while they may be (a router)',
  'tv:report': 'Report TVs trying to go online and their power (Home Assistant, a router)',
};

/** Which scopes each kind is offered. An assistant acts as you, for every child. */
export const kindScopes: Record<CredentialKind, string[]> = {
  service: ['household:read', 'children:read', 'screentime:read', 'learning:award', 'games:gate', 'tv:gate', 'tv:report'],
  assistant: [
    'household:read', 'children:read', 'today:read', 'today:complete', 'inbox:read', 'inbox:decide',
    'points:read', 'rewards:read', 'calendar:read', 'meals:read', 'screentime:read', 'screentime:grant', 'tv:read', 'tv:manage', 'tv:control',
  ],
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
  { id: 'k1', kind: 'service', name: 'Home server', scopes: ['household:read', 'children:read'], allChildren: true, childIds: [], createdAt: 'Sep 2', awardRuleIds: [] },
  { id: 'k2', kind: 'assistant', name: 'Kitchen assistant', scopes: ['children:read'], allChildren: false, childIds: ['c1', 'c2'], createdAt: 'Sep 9', expiresAt: 'Dec 9', awardRuleIds: [] },
  { id: 'k4', kind: 'service', name: 'Minecraft server', scopes: ['learning:award'], allChildren: false, childIds: ['c1', 'c2'], createdAt: 'Sep 12', awardRuleIds: ['r1', 'r2'] },
];

export const issued: CreateCredentialResponse = {
  credential: { id: 'k3', kind: 'service', name: 'Router', scopes: ['children:read'], allChildren: true, childIds: [], createdAt: 'Today', awardRuleIds: [] },
  secret: 'ocs_7Kq2XmP4vN8rT3wY6zB9cD5fH1jL0aE',
};
