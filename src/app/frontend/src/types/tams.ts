export interface Source {
  id: string;
  format: string;
  label?: string;
  description?: string;
  tags?: Record<string, string | string[]>;
  created?: string;
  updated?: string;
  created_by?: string;
  updated_by?: string;
  source_collection?: SourceCollectionItem[];
  collected_by?: string[];
}

export interface SourceCollectionItem {
  id: string;
  role?: string;
}

export interface FlowEssenceParameters {
  frame_width?: number;
  frame_height?: number;
  frame_rate?: { numerator: number; denominator?: number };
  bit_depth?: number;
  interlace_mode?: string;
  colorspace?: string;
  transfer_characteristic?: string;
  component_type?: string;
  sample_rate?: number;
  channels?: number;
  vfr?: boolean;
}

export interface Flow {
  id: string;
  source_id: string;
  label?: string;
  description?: string;
  format?: string;
  codec?: string;
  container?: string;
  tags?: Record<string, string | string[]>;
  created?: string;
  created_by?: string;
  updated_by?: string;
  metadata_updated?: string;
  segments_updated?: string;
  generation?: number;
  read_only?: boolean;
  avg_bit_rate?: number;
  max_bit_rate?: number;
  timerange?: string;
  essence_parameters?: FlowEssenceParameters;
  segment_duration?: { numerator: number; denominator: number };
  flow_collection?: FlowCollectionItem[];
  collected_by?: string[];
  metadata_version?: string;
}

export interface FlowCollectionItem {
  id: string;
  role?: string;
}

export interface FlowSegment {
  object_id: string;
  timerange: string;
  ts_offset?: string;
  object_timerange?: string;
  last_duration?: string;
  sample_offset?: number;
  sample_count?: number;
  key_frame_count?: number;
  get_urls?: ObjectUrl[];
}

export type FlowSegmentWrite = Pick<
  FlowSegment,
  | "object_id"
  | "timerange"
  | "ts_offset"
  | "object_timerange"
  | "last_duration"
  | "sample_offset"
  | "sample_count"
  | "key_frame_count"
> & {
  get_urls?: Array<{
    url: string;
    label: string;
  }>;
};

export interface ObjectUrl {
  url: string;
  storage_id?: string;
  presigned?: boolean;
  label?: string;
  controlled?: boolean;
  store_type?: string;
  provider?: string;
  region?: string;
}

export interface MediaObject {
  id: string;
  referenced_by_flows?: string[];
  first_referenced_by_flow?: string;
  timerange?: string;
  get_urls?: ObjectUrl[];
  key_frame_count?: number;
}

export interface StorageBackend {
  id?: string;
  store_type?: string;
  provider?: string;
  region?: string;
  availability_zone?: string;
  store_product?: string;
  label?: string;
  default_storage?: boolean;
}

export interface EventStreamMechanism {
  name?: string;
  docs?: string | null;
  config?: unknown;
}

export interface ServiceInfo {
  type?: string;
  api_version?: string;
  name?: string;
  description?: string;
  service_version?: string;
  event_stream_mechanisms?: EventStreamMechanism[];
  min_object_timeout?: string;
  min_presigned_url_timeout?: string;
}

export interface UpdateServiceInfo {
  name?: string;
  description?: string;
}

export interface ErrorStatusMetadata {
  type?: string;
  summary?: string;
  time?: string;
}

export interface DeletionRequest {
  id: string;
  flow_id: string;
  timerange_to_delete: string;
  timerange_remaining?: string;
  delete_flow: boolean;
  status: "created" | "started" | "done" | "error";
  created?: string;
  created_by?: string;
  updated?: string;
  expiry?: string;
  error?: ErrorStatusMetadata;
}

export type WebhookError = ErrorStatusMetadata;

export interface WebhookDetail {
  id?: string;
  url: string;
  api_key_name?: string;
  events: string[];
  flow_ids?: string[];
  source_ids?: string[];
  flow_collected_by_ids?: string[];
  source_collected_by_ids?: string[];
  accept_get_urls?: string[];
  accept_storage_ids?: string[];
  presigned?: boolean;
  verbose_storage?: boolean;
  tags?: Record<string, string | string[]>;
  status?: "created" | "started" | "disabled" | "error";
  error?: WebhookError;
}

export interface WebhookWritePayload {
  url: string;
  api_key_name?: string;
  api_key_value?: string;
  events: string[];
  flow_ids?: string[];
  source_ids?: string[];
  flow_collected_by_ids?: string[];
  source_collected_by_ids?: string[];
  accept_get_urls?: string[];
  accept_storage_ids?: string[];
  presigned?: boolean;
  verbose_storage?: boolean;
  tags?: Record<string, string | string[]>;
  status?: "created" | "disabled";
}

export interface StorageAllocation {
  media_objects: Array<{
    object_id: string;
    put_url: HttpRequest;
  }>;
}

export interface HttpRequest {
  url: string;
  body?: string;
  "content-type"?: string;
  headers?: Record<string, string>;
}

export interface PaginatedResponse<T> {
  data: T[];
  nextKey?: string;
  limit?: number;
}

export interface PaginatedResourceResponse<T> {
  data: T;
  nextKey?: string;
  limit?: number;
}
