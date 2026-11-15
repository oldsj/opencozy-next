/**
 * Slice 6 (learning), typed to opencozy-pilot's shapes (fictional data). A learning service (such as
 * a Minecraft server) awards points by activity key, under each rule's daily limit, for children
 * linked to a player ID.
 */
export interface AwardRule {
  id: string;
  /** Stable key the service sends, such as `quest.complete`. Unique among live rules (rule_exists). */
  activityKey: string;
  points: number;
  /** Most points this rule can give one child in a household day. */
  dailyLimit: number;
  version: number;
  archived: boolean;
}

export type LearningProvider = 'minecraft';

export interface LearningLink {
  id: string;
  childId: string;
  provider: LearningProvider;
  /** The player ID on that service; one child per player ID (link_exists). */
  externalId: string;
  linkedAt: string;
}

export const providerNames: Record<LearningProvider, string> = { minecraft: 'Minecraft' };

export const awardRules: AwardRule[] = [
  { id: 'r1', activityKey: 'quest.complete', points: 5, dailyLimit: 20, version: 2, archived: false },
  { id: 'r2', activityKey: 'lesson.finish', points: 3, dailyLimit: 15, version: 1, archived: false },
  { id: 'r3', activityKey: 'build.share', points: 2, dailyLimit: 6, version: 3, archived: true },
];

export const learningLinks: LearningLink[] = [
  { id: 'l1', childId: 'c1', provider: 'minecraft', externalId: 'AdaBuilds', linkedAt: 'Sep 5' },
  { id: 'l2', childId: 'c2', provider: 'minecraft', externalId: 'EzraCrafts', linkedAt: 'Sep 6' },
];

/** The API's error codes, in words for adults. */
export const learningErrorText = {
  rule_exists: 'A rule for that activity already exists. Edit it instead.',
  link_exists: 'That player ID is already linked to a child.',
  recent_auth_required: 'For your security, sign in again to add a key. It only takes a moment.',
} as const;
