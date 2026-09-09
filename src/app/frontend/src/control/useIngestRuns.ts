import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import {
  ConsoleApiError,
  cancelIngestRun,
  getConsoleSession,
  getIngestRun,
  type IngestRunDetail,
} from "@/control/ingest";

const sessionKey = ["control", "session"] as const;
export const ingestRunsKey = ["control", "ingest-runs"] as const;

export function ingestRunKey(name: string) {
  return [...ingestRunsKey, name] as const;
}

export function useConsoleSession() {
  return useQuery({
    queryKey: sessionKey,
    queryFn: ({ signal }) => getConsoleSession({ signal }),
    retry: false,
    staleTime: 30_000,
  });
}

export function useIngestRun(name: string) {
  return useQuery({
    queryKey: ingestRunKey(name),
    queryFn: ({ signal }) => getIngestRun(name, { signal }),
    enabled: Boolean(name),
    retry: false,
    staleTime: 5_000,
  });
}

export function useCancelIngestRun() {
  const queryClient = useQueryClient();
  const activeRequest = useRef<AbortController | null>(null);

  useEffect(() => () => activeRequest.current?.abort(), []);

  return useMutation({
    mutationFn: async (run: IngestRunDetail) => {
      activeRequest.current?.abort();
      const controller = new AbortController();
      activeRequest.current = controller;
      return cancelIngestRun(
        run.name,
        { uid: run.uid, revision: run.revision },
        { signal: controller.signal },
      );
    },
    onSuccess: ({ run }) => {
      queryClient.setQueryData(ingestRunKey(run.name), run);
      void queryClient.invalidateQueries({ queryKey: ingestRunsKey });
    },
    onError: (error, run) => {
      if (error instanceof ConsoleApiError && error.status === 409) {
        void queryClient.invalidateQueries({
          queryKey: ingestRunKey(run.name),
        });
      }
    },
    onSettled: () => {
      activeRequest.current = null;
    },
  });
}
