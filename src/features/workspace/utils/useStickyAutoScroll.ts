import { useCallback, useLayoutEffect, useRef } from 'react';

/**
 * Keeps a scrollable log container pinned to its bottom synchronously before browser paint,
 * avoiding post-paint scroll snaps and respecting intentional scroll-up by the user.
 */
export function useStickyAutoScroll(
  containerRef: React.RefObject<HTMLDivElement | null>,
  enabled: boolean,
  tailKey: string
) {
  const pinnedRef = useRef(true);
  const prevEnabledRef = useRef(enabled);

  if (enabled && !prevEnabledRef.current) {
    pinnedRef.current = true;
  }
  prevEnabledRef.current = enabled;

  const onScroll = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    pinnedRef.current = el.scrollHeight - el.scrollTop - el.clientHeight <= 48;
  }, [containerRef]);

  useLayoutEffect(() => {
    if (!enabled || !pinnedRef.current) return;
    const el = containerRef.current;
    if (!el) return;
    const target = el.scrollHeight - el.clientHeight;
    if (target > 0 && Math.abs(el.scrollTop - target) > 1) {
      el.scrollTop = target;
    }
  }, [containerRef, enabled, tailKey]);

  return { onScroll };
}
