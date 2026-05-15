import type { FlowSegment, FlowSegmentWrite } from "@/types/tams";

export function segmentToWritePayload(segment: FlowSegment): FlowSegmentWrite {
  const payload: FlowSegmentWrite = {
    object_id: segment.object_id,
    timerange: segment.timerange,
  };

  if (segment.ts_offset !== undefined) payload.ts_offset = segment.ts_offset;
  if (segment.object_timerange !== undefined) {
    payload.object_timerange = segment.object_timerange;
  }
  if (segment.last_duration !== undefined) {
    payload.last_duration = segment.last_duration;
  }
  if (segment.sample_offset !== undefined) {
    payload.sample_offset = segment.sample_offset;
  }
  if (segment.sample_count !== undefined) {
    payload.sample_count = segment.sample_count;
  }
  if (segment.key_frame_count !== undefined) {
    payload.key_frame_count = segment.key_frame_count;
  }

  const externalUrls = segment.get_urls
    ?.filter(
      (entry): entry is { url: string; label: string; controlled: false } =>
        entry.controlled === false && Boolean(entry.label),
    )
    .map((entry) => ({ url: entry.url, label: entry.label }));
  if (externalUrls?.length) {
    payload.get_urls = externalUrls;
  }

  return payload;
}
