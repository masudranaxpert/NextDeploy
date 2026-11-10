import { startTransition } from 'react';
import { flushSync } from 'react-dom';

type DocumentWithVT = Document & {
  startViewTransition?: (cb: () => void) => { finished: Promise<void> };
};

/**
 * Runs a UI navigation state update inside a native browser View Transition when supported
 * and reduced-motion is not active; falls back cleanly to React startTransition otherwise.
 */
export function runWithViewTransition(update: () => void): void {
  if (typeof document === 'undefined') {
    update();
    return;
  }
  const doc = document as DocumentWithVT;
  const reducedMotion =
    typeof window !== 'undefined' &&
    window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;

  if (typeof doc.startViewTransition === 'function' && !reducedMotion) {
    try {
      doc.startViewTransition(() => {
        flushSync(update);
      });
      return;
    } catch {
      // Fall through to standard React transition if view transition cannot capture snapshot.
    }
  }
  startTransition(update);
}
