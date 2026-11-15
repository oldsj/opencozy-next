/** Joins class names, skipping empty ones. A caller's className extends a component's classes. */
export function cx(...names: (string | false | null | undefined)[]): string | undefined {
  const value = names.filter(Boolean).join(' ');
  return value || undefined;
}
