import { QueryClient } from "@tanstack/react-query";

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
