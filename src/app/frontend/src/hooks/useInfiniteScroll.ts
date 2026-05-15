import { useEffect, useRef } from "react";

export function useInfiniteScroll(
  enabled: boolean,
  onIntersect: () => void,
  rootMargin = "200px",
) {
  const sentinelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!enabled) return;
    const sentinel = sentinelRef.current;
    if (!sentinel || typeof IntersectionObserver === "undefined") return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          onIntersect();
        }
      },
      { rootMargin },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [enabled, onIntersect, rootMargin]);

  return sentinelRef;
}
