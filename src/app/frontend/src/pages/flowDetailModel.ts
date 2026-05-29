import type { DeletionRequest, FlowSegment } from "@/types/tams";

export const SEGMENT_PAGE_SIZE = "300";

export function estimateDeleteScope(
  segments: FlowSegment[],
  hasMoreSegments: boolean,
): string {
  if (!segments.length && !hasMoreSegments) {
    return "This will remove the flow record only.";
  }
  if (hasMoreSegments) {
    return `This will delete the flow and at least ${segments.length} loaded segment${segments.length === 1 ? "" : "s"}. Additional segments may exist beyond the current page.`;
  }
  if (segments.length === 1) {
    return "This will delete the flow and 1 registered segment.";
  }
  return `This will delete the flow and ${segments.length} registered segments.`;
}

export function deletionRequestPath(request: DeletionRequest): string {
  return `/deletions?request=${encodeURIComponent(request.id)}`;
}

export function segmentStorageSummary(segment: FlowSegment): string {
  const labels =
    segment.get_urls
      ?.map((entry) => entry.label ?? entry.storage_id)
      .filter((entry): entry is string => Boolean(entry)) ?? [];
  if (labels.length === 0) return `${segment.get_urls?.length ?? 0} URL(s)`;
  return Array.from(new Set(labels)).join(", ");
}
