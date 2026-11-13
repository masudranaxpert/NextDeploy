import { startTransition } from 'react';

/**
 * Runs a UI navigation state update via React startTransition without whole-viewport
 * bitmap crossfading, preventing 1px hairline border flicker during route/data loads.
 */
export function runWithViewTransition(update: () => void): void {
  if (typeof document === 'undefined') {
    update();
    return;
  }
  startTransition(update);
}
