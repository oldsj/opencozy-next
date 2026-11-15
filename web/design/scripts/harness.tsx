import type { ComponentType } from 'react';

// Preview a surface or theme: ?surface=kiosk|tv and ?theme=light|dark, as the app sets them on <html>.
const params = new URLSearchParams(window.location.search);
for (const key of ['surface', 'theme'] as const) {
  const value = params.get(key);
  if (value && /^[a-z]+$/.test(value)) document.documentElement.dataset[key] = value;
}

/** Renders every story a preview file exports, each under its own labelled heading. */
export function Stories({ name, stories }: { name: string; stories: Record<string, unknown> }) {
  const entries = Object.entries(stories).filter(
    (entry): entry is [string, ComponentType] => typeof entry[1] === 'function',
  );
  return (
    <main className="mx-auto max-w-3xl space-y-8 p-6">
      <h1 className="text-section">{name}</h1>
      {entries.map(([story, Story]) => (
        <section key={story} aria-labelledby={`story-${story}`} data-story={story}>
          <h2 id={`story-${story}`} className="text-muted-foreground text-meta mb-3 font-sans">
            {story}
          </h2>
          <Story />
        </section>
      ))}
    </main>
  );
}
