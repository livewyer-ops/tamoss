import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { config } from "@/config";

export interface RuntimeCondition {
  type: string;
  status: string;
  reason?: string;
  message?: string;
  observedGeneration?: number;
  lastTransitionTime?: string;
}

export interface RuntimeResourceCondition {
  type: string;
  status: string;
  reason?: string;
  message?: string;
  lastTransitionTime?: string;
}

export interface RuntimeServicePort {
  name?: string;
  protocol: string;
  port: number;
  targetPort: string;
}

export interface RuntimeService {
  name: string;
  component?: string;
  type: string;
  selectorComponent?: string;
  ports: RuntimeServicePort[];
}

export interface RuntimeEndpointSlicePort {
  name?: string;
  protocol?: string;
  port?: number;
}

export interface RuntimeEndpointSlice {
  name: string;
  serviceName: string;
  component?: string;
  addressType: string;
  ports: RuntimeEndpointSlicePort[];
  totalEndpoints: number;
  readyEndpoints: number;
  notReadyEndpoints: number;
  terminatingEndpoints: number;
}

export interface RuntimeSnapshot {
  schemaVersion: "1.0";
  observedAt: string;
  stale: boolean;
  instance: {
    name: string;
    namespace: string;
    uid: string;
    generation: number;
    observedGeneration: number;
    phase: string;
    conditions: RuntimeCondition[];
  };
  workloads: Array<{
    kind: "Deployment";
    name: string;
    component?: string;
    status: "ready" | "progressing" | "unavailable" | "scaledDown";
    generation: number;
    observedGeneration: number;
    desiredReplicas: number;
    readyReplicas: number;
    availableReplicas: number;
    updatedReplicas: number;
    conditions: RuntimeResourceCondition[];
  }>;
  services: RuntimeService[];
  endpointSlices: RuntimeEndpointSlice[];
  pods: Array<{
    name: string;
    component?: string;
    phase: string;
    ready: boolean;
    restarts: number;
    reason?: string;
    message?: string;
    startedAt?: string;
    deleting: boolean;
  }>;
  jobs: Array<{
    name: string;
    component?: string;
    status: "pending" | "running" | "succeeded" | "failed" | "suspended";
    active: number;
    succeeded: number;
    failed: number;
    startTime?: string;
    completionTime?: string;
    conditions: RuntimeResourceCondition[];
  }>;
  events: Array<{
    type: string;
    reason?: string;
    message?: string;
    regarding: { kind: string; name: string };
    count: number;
    firstObservedAt?: string;
    lastObservedAt?: string;
  }>;
}

const runtimeKey = ["control", "runtime"] as const;

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
    return (await response.json()) as RuntimeSnapshot;
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
        queryClient.setQueryData(runtimeKey, JSON.parse(event.data));
      } catch {
        // Polling remains available if an individual event is malformed.
      }
    };
    events.addEventListener("runtime", update as EventListener);
    return () => events.close();
  }, [runtimeAvailable, queryClient]);

  return query;
}
