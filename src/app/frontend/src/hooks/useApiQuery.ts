import { useCallback, useId, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { apiQueryKeys, apiQueryPolicy } from "@/api/query";

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
  const queryScope = useId();
  const queryKey = useMemo(
    () => apiQueryKeys.scoped(queryScope, deps),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [queryScope, ...deps],
  );
  const query = useQuery<T, Error>({
    queryKey,
    queryFn,
    ...apiQueryPolicy,
  });
  const refetch = useCallback(() => {
    void query.refetch();
  }, [query]);

  return {
    data: query.data ?? null,
    loading: query.isLoading || (query.isFetching && query.data === undefined),
    error: query.error?.message ?? null,
    refetch,
  };
}
