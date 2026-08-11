import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { config } from "@/config";
import type { components, operations } from "@/control/generated/openapi";

export type RuntimeCondition =
  components["schemas"]["RuntimeInstanceCondition"];
export type RuntimeResourceCondition =
  components["schemas"]["RuntimeResourceCondition"];
export type RuntimeServicePort = components["schemas"]["RuntimeServicePort"];
export type RuntimeService = components["schemas"]["RuntimeService"];
export type RuntimeEndpointSlicePort =
  components["schemas"]["RuntimeEndpointSlicePort"];
export type RuntimeEndpointSlice =
  components["schemas"]["RuntimeEndpointSlice"];
export type RuntimeSnapshot =
  operations["getRuntimeSnapshot"]["responses"][200]["content"]["application/json"];

const runtimeKey = ["control", "runtime"] as const;

function collection<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

function normalizeRuntimeSnapshot(snapshot: RuntimeSnapshot): RuntimeSnapshot {
  return {
    ...snapshot,
    instance: {
      ...snapshot.instance,
      conditions: collection(snapshot.instance.conditions),
    },
    workloads: collection(snapshot.workloads).map((workload) => ({
      ...workload,
      conditions: collection(workload.conditions),
    })),
    services: collection(snapshot.services).map((service) => ({
      ...service,
      ports: collection(service.ports),
    })),
    endpointSlices: collection(snapshot.endpointSlices).map((slice) => ({
      ...slice,
      ports: collection(slice.ports),
    })),
    pods: collection(snapshot.pods),
    jobs: collection(snapshot.jobs).map((job) => ({
      ...job,
      conditions: collection(job.conditions),
    })),
    events: collection(snapshot.events),
  };
}

function controlUrl(path: string): string {
  const base = config.controlApiUrl.endsWith("/")
    ? config.controlApiUrl
    : `${config.controlApiUrl}/`;
  return new URL(
    path.replace(/^\//, ""),
    new URL(base, window.location.origin),
  ).toString();
}

async function getRuntime(): Promise<RuntimeSnapshot> {
  const response = await fetch(controlUrl("runtime"), {
    credentials: "same-origin",
    headers: { Accept: "application/json" },
  });
  if (!response.ok) {
    throw new Error("Runtime status is unavailable.");
  }
  try {
    return normalizeRuntimeSnapshot((await response.json()) as RuntimeSnapshot);
  } catch {
    throw new Error("Runtime status is unavailable.");
  }
}

export function useRuntime() {
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: runtimeKey,
    queryFn: getRuntime,
    retry: false,
    staleTime: 15_000,
    refetchInterval: 30_000,
  });
  const runtimeAvailable = Boolean(query.data);

  useEffect(() => {
    if (!runtimeAvailable || typeof EventSource === "undefined") return;
    const events = new EventSource(controlUrl("runtime/events"), {
      withCredentials: true,
    });
    const update = (event: MessageEvent<string>) => {
      try {
        queryClient.setQueryData(
          runtimeKey,
          normalizeRuntimeSnapshot(JSON.parse(event.data) as RuntimeSnapshot),
        );
      } catch {
        // Polling remains available if an individual event is malformed.
      }
    };
    events.addEventListener("runtime", update as EventListener);
    return () => events.close();
  }, [runtimeAvailable, queryClient]);

  return query;
}
