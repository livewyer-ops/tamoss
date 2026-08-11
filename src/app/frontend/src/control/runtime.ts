import {
  replaceEqualDeep,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
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

function observationTime(snapshot: RuntimeSnapshot): number | undefined {
  const observedAt = Date.parse(snapshot.observedAt);
  return Number.isNaN(observedAt) ? undefined : observedAt;
}

/**
 * Resolves the race between the recovery poll and the event stream.
 *
 * Both write the same cache entry, so a poll that started before a streamed
 * update can land after it. Every snapshot records when the Console API
 * observed cluster state, so an older observation never replaces a newer one.
 * Snapshots without a comparable observation time keep last-write-wins.
 */
function newestRuntimeSnapshot(
  previous: RuntimeSnapshot | undefined,
  next: RuntimeSnapshot,
): RuntimeSnapshot {
  const previousTime = previous ? observationTime(previous) : undefined;
  const nextTime = observationTime(next);
  if (
    previous !== undefined &&
    previousTime !== undefined &&
    nextTime !== undefined &&
    nextTime < previousTime
  ) {
    return previous;
  }
  return replaceEqualDeep(previous, next);
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
    // Applied to every cache write, so it orders both poll responses and
    // streamed updates.
    structuralSharing: (previous, next) =>
      newestRuntimeSnapshot(
        previous as RuntimeSnapshot | undefined,
        next as RuntimeSnapshot,
      ),
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
