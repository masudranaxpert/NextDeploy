/**
 * Smart Data Sync & Reactivity Bus for NextDeploy
 *
 * Implements SWR (Stale-While-Revalidate) with:
 * 1. Automatic in-flight request deduplication (prevents concurrent duplicate calls)
 * 2. Strict cooldown / staleTime (prevents polling storms and DB hammering)
 * 3. Mutation-driven invalidation (instant 0ms update on create/delete/modify)
 * 4. Visibility & route-change revalidation (updates stale tabs without spamming)
 */

type Listener = () => void;

class DataSyncManager {
  private inFlightBootstrap: Promise<any> | null = null;
  private lastBootstrapTime = 0;
  // Data is considered fresh for 15 seconds; route changes within 15s use cache with 0 network calls.
  private staleTimeMs = 15_000;
  // Window focus check has a 30s cooldown to prevent spamming on rapid alt-tabs.
  private focusCooldownMs = 30_000;
  private lastFocusRevalidate = 0;

  private listeners = new Set<Listener>();

  public subscribe(listener: Listener): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  public notify(): void {
    this.listeners.forEach((fn) => {
      try {
        fn();
      } catch (err) {
        console.error('[dataSync] listener error:', err);
      }
    });
  }

  /**
   * Revalidates global bootstrap data with deduplication and cooldown.
   * If force=true (e.g., after a mutation like create/delete), cooldown is bypassed.
   */
  public async revalidateBootstrap(
    fetcher: () => Promise<any>,
    onData: (data: any) => void,
    force = false
  ): Promise<boolean> {
    const now = Date.now();

    // 1. Cooldown guard: if data is fresh and not forced, skip network call entirely
    if (!force && this.lastBootstrapTime > 0 && now - this.lastBootstrapTime < this.staleTimeMs) {
      return false;
    }

    // 2. Request deduplication: if a request is already in-flight, reuse it
    if (this.inFlightBootstrap) {
      try {
        const data = await this.inFlightBootstrap;
        if (data) onData(data);
        return true;
      } catch {
        return false;
      }
    }

    // 3. Fire single deduplicated request
    this.inFlightBootstrap = (async () => {
      try {
        const res = await fetcher();
        if (res?.ok && res?.data) {
          this.lastBootstrapTime = Date.now();
          onData(res.data);
          this.notify();
          return res.data;
        }
        return null;
      } finally {
        this.inFlightBootstrap = null;
      }
    })();

    const result = await this.inFlightBootstrap;
    return Boolean(result);
  }

  /**
   * Called on route changes. Only revalidates if staleTime has elapsed.
   */
  public checkRouteRevalidate(fetcher: () => Promise<any>, onData: (data: any) => void): void {
    const now = Date.now();
    if (this.lastBootstrapTime > 0 && now - this.lastBootstrapTime < this.staleTimeMs) {
      return; // Data is fresh, zero network calls
    }
    void this.revalidateBootstrap(fetcher, onData, false);
  }

  /**
   * Called on window focus. Uses a larger 30s cooldown.
   */
  public checkFocusRevalidate(fetcher: () => Promise<any>, onData: (data: any) => void): void {
    const now = Date.now();
    if (now - this.lastFocusRevalidate < this.focusCooldownMs) {
      return;
    }
    this.lastFocusRevalidate = now;
    void this.revalidateBootstrap(fetcher, onData, false);
  }

  /**
   * Call after any Create / Delete / Update mutation to invalidate cache and refresh immediately.
   */
  public invalidate(fetcher?: () => Promise<any>, onData?: (data: any) => void): void {
    this.lastBootstrapTime = 0; // mark stale
    if (fetcher && onData) {
      void this.revalidateBootstrap(fetcher, onData, true);
    } else {
      notifyMutation();
    }
  }
}

export const dataSync = new DataSyncManager();

/**
 * Global helper to notify all subscribers and tabs that a mutation happened.
 * Dispatches the 'nd:invalidate' custom event.
 */
export function notifyMutation(): void {
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent('nd:invalidate'));
  }
}
