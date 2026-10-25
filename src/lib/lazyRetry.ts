import { lazy, type ComponentType } from 'react';

const RELOAD_KEY = 'nd_chunk_reload_at';

/**
 * Reloads the page once when a piece of the app cannot be downloaded. That happens when the panel
 * was updated while this tab stayed open: the old file names are gone, the new page has the new ones.
 * A guard stops a reload loop when the server itself is down.
 */
export function reloadForNewVersion(): boolean {
  try {
    const last = Number(sessionStorage.getItem(RELOAD_KEY) || 0);
    if (Date.now() - last < 15000) return false;
    sessionStorage.setItem(RELOAD_KEY, String(Date.now()));
  } catch {
    // storage blocked: still reload once, the browser will not loop on its own
  }
  window.location.reload();
  return true;
}

// Auto-recover stale tabs when a chunk hash was replaced after a server deployment
if (typeof window !== 'undefined') {
  window.addEventListener('error', (event) => {
    if (
      event?.message &&
      (event.message.includes('dynamically imported module') ||
        event.message.includes('Loading chunk') ||
        event.message.includes('Failed to fetch'))
    ) {
      reloadForNewVersion();
    }
  });
  window.addEventListener('unhandledrejection', (event) => {
    const msg = String(event?.reason?.message || event?.reason || '');
    if (
      msg.includes('dynamically imported module') ||
      msg.includes('Loading chunk') ||
      msg.includes('Failed to fetch')
    ) {
      reloadForNewVersion();
    }
  });
}

/** React.lazy that recovers from a missing chunk after an update. */
export function lazyRetry<T extends ComponentType<any>>(factory: () => Promise<{ default: T }>) {
  return lazy(async () => {
    try {
      return await factory();
    } catch (err) {
      if (reloadForNewVersion()) return new Promise<{ default: T }>(() => {}); // page is reloading
      throw err;
    }
  });
}
