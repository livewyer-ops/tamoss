import { QueryClient, type QueryKey } from "@tanstack/react-query";

export const apiQueryKeys = {
  all: ["api"] as const,
  scoped(scope: string, parts: readonly unknown[] = []): QueryKey {
    return [...apiQueryKeys.all, scope, ...parts];
  },
};

export const apiQueryPolicy = {
  retry: 1,
  staleTime: 30_000,
  refetchOnWindowFocus: false,
} as const;

export const apiQueryClient = new QueryClient({
  defaultOptions: {
    queries: apiQueryPolicy,
    mutations: {
      retry: 0,
    },
  },
});
