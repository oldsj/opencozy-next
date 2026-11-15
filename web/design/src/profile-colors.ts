/** The eight stable profile colours. A key belongs to a profile, not to its position in a list. */
export const profileColors = ['rose', 'sage', 'sky', 'butter', 'lilac', 'peach', 'teal', 'sand'] as const;

export type ProfileColor = (typeof profileColors)[number];

/** The class that gives a subtree its person context (--profile-soft, -base, -ink, -line, -track). */
export function profileClass(color: ProfileColor): string {
  return `oc-profile--${color}`;
}

/** The first letter of a name, for avatars and person marks. */
export function initialOf(name: string): string {
  return name.trim().charAt(0).toUpperCase() || '?';
}
