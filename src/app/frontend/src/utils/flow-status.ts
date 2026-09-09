import type { FlowStatus } from "@/types/tams";

const labels: Record<FlowStatus, string> = {
  awaiting_content: "Awaiting content",
  ingesting: "Ingesting",
  replication_in_progress: "Replicating",
  closed_complete: "Closed complete",
};

export function flowStatusLabel(status?: FlowStatus): string {
  return status ? labels[status] : "Not set";
}

export function flowStatusTone(
  status?: FlowStatus,
): "neutral" | "success" | "warning" | "info" {
  if (status === "closed_complete") return "success";
  if (status === "ingesting" || status === "replication_in_progress") {
    return "warning";
  }
  if (status === "awaiting_content") return "info";
  return "neutral";
}
