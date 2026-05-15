import { useState, useEffect, useCallback, useRef } from "react";

interface QueryState<T> {
  data: T | null;
  loading: boolean;
  error: string | null;
}

interface QueryResult<T> extends QueryState<T> {
  refetch: () => void;
}

export function useApiQuery<T>(
  queryFn: () => Promise<T>,
  deps: unknown[] = [],
): QueryResult<T> {
  const [state, setState] = useState<QueryState<T>>({
    data: null,
    loading: true,
    error: null,
  });

  const mountedRef = useRef(true);

  const execute = useCallback(async () => {
    setState((prev) => ({ ...prev, loading: true, error: null }));
    try {
      const data = await queryFn();
      if (mountedRef.current) {
        setState({ data, loading: false, error: null });
      }
    } catch (err) {
      if (mountedRef.current) {
        setState({
          data: null,
          loading: false,
          error: err instanceof Error ? err.message : "Unknown error",
        });
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  useEffect(() => {
    mountedRef.current = true;
    execute();
    return () => {
      mountedRef.current = false;
    };
  }, [execute]);

  return { ...state, refetch: execute };
}
