import type { components } from "@/api/generated/openapi";

type ApiSchemas = components["schemas"];
type ApiTagMap = Record<string, string | string[]>;

export type SourceCollectionItem = Omit<
  ApiSchemas["collection-item"],
  "role"
> & {
  role?: string;
};

export type Source = Omit<
  ApiSchemas["source"],
  "format" | "tags" | "source_collection" | "collected_by"
> & {
  format: string;
  tags?: ApiTagMap;
  source_collection?: SourceCollectionItem[];
  collected_by?: string[];
};

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
  init_segments?: boolean;
}

export type FlowCollectionItem = Omit<
  ApiSchemas["flow-collection"][number],
  "role"
> & {
  role?: string;
};

export type Flow = Omit<
  ApiSchemas["flow-get"],
  | "format"
  | "tags"
  | "essence_parameters"
  | "segment_duration"
  | "flow_collection"
  | "collected_by"
> & {
  format?: string;
  codec?: string;
  container?: string;
  avg_bit_rate?: number;
  container_mapping?: ApiSchemas["container-mapping"];
  tags?: ApiTagMap;
  essence_parameters?: FlowEssenceParameters;
  segment_duration?: { numerator: number; denominator: number };
  flow_collection?: FlowCollectionItem[];
  collected_by?: string[];
};

export type FlowSegment = ApiSchemas["flow-segment"];
export type FlowSegmentWrite = ApiSchemas["flow-segment-post"];
export type ObjectUrl = NonNullable<
  ApiSchemas["object-core"]["get_urls"]
>[number];
export type MediaObject = ApiSchemas["object"];
export type StorageBackend = ApiSchemas["storage-backends-list"][number];
export type ServiceInfo = ApiSchemas["service"];
export type UpdateServiceInfo = ApiSchemas["service-post"];
export type DeletionRequest = ApiSchemas["deletion-request"];
export type WebhookDetail = ApiSchemas["webhook-get"];
export type WebhookWritePayload = ApiSchemas["webhook-post"];
export type WebhookEvent = WebhookWritePayload["events"][number];
export type HttpRequest = ApiSchemas["http-request"];
type ContractStorageAllocation = ApiSchemas["flow-storage"];
type ContractStorageMediaObject = NonNullable<
  ContractStorageAllocation["media_objects"]
>[number];
export type StorageAllocation = Omit<
  ContractStorageAllocation,
  "media_objects"
> & {
  media_objects: Array<
    ContractStorageMediaObject & {
      storage_id?: string | null;
    }
  >;
};

export interface StorageAllocationOptions {
  storageId?: string;
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
