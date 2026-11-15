import { useEffect } from 'react';

export type Surface = 'kiosk' | 'tv';

/**
 * Marks the whole document as a surface while the calling route is shown: `<html
 * data-surface="kiosk">` scales the root size and switches the kiosk layout (DESIGN.md). It is
 * removed again on unmount, so the adult pages in the same app are unaffected. Sets a DOM
 * attribute only, so it is CSP-safe.
 */
export function useSurface(surface: Surface): void {
  useEffect(() => {
    const root = document.documentElement;
    const previous = root.dataset.surface;
    root.dataset.surface = surface;
    return () => {
      if (previous === undefined) delete root.dataset.surface;
      else root.dataset.surface = previous;
    };
  }, [surface]);
}
