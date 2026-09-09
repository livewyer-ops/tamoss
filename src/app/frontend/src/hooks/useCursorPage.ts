import { useCallback, useEffect, useRef, useState } from "react";

interface CursorResponse<T> {
  data: T[];
  nextKey?: string;
  limit?: number;
}

/**
 * Retained Previous cursors. Cursors are short opaque strings, so this bounds
 * memory well beyond the paging depth an operator reaches by hand while
 * keeping Previous available for the whole of that traversal.
 */
const MAX_CURSOR_HISTORY = 100;

export function useCursorPage<T>({
  cursor,
  load,
  onCursorChange,
}: {
  cursor?: string;
  load: (
    cursor: string | undefined,
    signal: AbortSignal,
  ) => Promise<CursorResponse<T>>;
  onCursorChange: (cursor?: string) => void;
}) {
  const [response, setResponse] = useState<CursorResponse<T>>({ data: [] });
  const [error, setError] = useState<Error | null>(null);
  const [loading, setLoading] = useState(true);
  const [history, setHistory] = useState<Array<string | undefined>>([]);
  const [requestVersion, setRequestVersion] = useState(0);
  const requestId = useRef(0);
  const previousCursor = useRef(cursor);
  const pendingCursor = useRef<{ value?: string } | null>(null);

  useEffect(() => {
    if (previousCursor.current === cursor) return;

    const pending = pendingCursor.current;
    pendingCursor.current = null;
    if (!pending || pending.value !== cursor) setHistory([]);
    previousCursor.current = cursor;
  }, [cursor]);

  // requestVersion intentionally makes Refresh repeat the current request.
  // biome-ignore lint/correctness/useExhaustiveDependencies: refresh trigger
  useEffect(() => {
    const controller = new AbortController();
    const id = ++requestId.current;
    setLoading(true);
    setError(null);
    load(cursor, controller.signal)
      .then((result) => {
        if (requestId.current === id) setResponse(result);
      })
      .catch((reason: unknown) => {
        if (requestId.current === id && !controller.signal.aborted) {
          setError(
            reason instanceof Error ? reason : new Error(String(reason)),
          );
        }
      })
      .finally(() => {
        if (requestId.current === id && !controller.signal.aborted) {
          setLoading(false);
        }
      });

    return () => controller.abort();
  }, [cursor, load, requestVersion]);

  const next = useCallback(() => {
    if (!response.nextKey) return;
    setHistory((current) => [...current, cursor].slice(-MAX_CURSOR_HISTORY));
    pendingCursor.current = { value: response.nextKey };
    onCursorChange(response.nextKey);
  }, [cursor, onCursorChange, response.nextKey]);

  const previous = useCallback(() => {
    const target = history[history.length - 1];
    setHistory((current) => current.slice(0, -1));
    pendingCursor.current = { value: target };
    onCursorChange(target);
  }, [history, onCursorChange]);

  const resetHistory = useCallback(() => {
    pendingCursor.current = null;
    setHistory([]);
  }, []);

  return {
    data: response.data,
    error,
    hasNext: Boolean(response.nextKey),
    hasPrevious: history.length > 0,
    loading,
    next,
    previous,
    refresh: () => setRequestVersion((value) => value + 1),
    resetHistory,
    serverLimit: response.limit,
  };
}
