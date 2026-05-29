export const DASHBOARD_COLLECTION_PAGE_SIZE = "50";

export function buildRecentActivity({
  flows,
  sources,
  deletions,
}: {
  flows: Array<{ id: string; label?: string; created?: string }>;
  sources: Array<{ id: string; label?: string; created?: string }>;
  deletions: Array<{
    id: string;
    flow_id: string;
    status: string;
    created?: string;
  }>;
}) {
  const items = [
    ...flows.map((flow) => ({
      key: `flow-${flow.id}`,
      title: flow.label || flow.id,
      subtitle: "Flow created",
      created: flow.created,
      to: `/flows/${flow.id}`,
      variant: "info" as const,
    })),
    ...sources.map((source) => ({
      key: `source-${source.id}`,
      title: source.label || source.id,
      subtitle: "Source discovered",
      created: source.created,
      to: `/sources/${source.id}`,
      variant: "default" as const,
    })),
    ...deletions.map((request) => ({
      key: `delete-${request.id}`,
      title: request.flow_id,
      subtitle: `Delete request ${request.status}`,
      created: request.created,
      to: `/deletions?request=${request.id}`,
      variant:
        request.status === "error" ? ("danger" as const) : ("warning" as const),
    })),
  ];

  return items
    .sort((left, right) => {
      const a = left.created ? Date.parse(left.created) : 0;
      const b = right.created ? Date.parse(right.created) : 0;
      return b - a;
    })
    .slice(0, 8);
}

export function getSystemState({
  healthError,
  serviceError,
  backendError,
  erroredWebhooks,
  activeDeletions,
}: {
  healthError: boolean;
  serviceError: boolean;
  backendError: boolean;
  erroredWebhooks: number;
  activeDeletions: number;
}) {
  if (healthError || serviceError || backendError) {
    return { label: "Failing", variant: "danger" as const };
  }
  if (erroredWebhooks > 0 || activeDeletions > 0) {
    return { label: "Degraded", variant: "warning" as const };
  }
  return { label: "Healthy", variant: "success" as const };
}
