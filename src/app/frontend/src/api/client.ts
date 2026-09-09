import {
  type ApiRequestOptions,
  ApiTransport,
  type PagingParams,
  path,
  type QueryParams,
} from "@/api/transport";
import type {
  DeletionRequest,
  Flow,
  FlowCollectionItem,
  FlowSegment,
  MediaObject,
  PaginatedResourceResponse,
  PaginatedResponse,
  Profile,
  ServiceInfo,
  Source,
  StorageBackend,
  WebhookDetail,
} from "@/types/tams";

export { ApiError } from "@/api/transport";

export type SourceListParams = PagingParams;

export type FlowListParams = PagingParams & {
  profile_id?: string;
  source_id?: string;
  status?:
    | "awaiting_content"
    | "ingesting"
    | "replication_in_progress"
    | "closed_complete";
  timerange?: string;
};

export type ProfileListParams = PagingParams & {
  codec?: string;
  format?: string;
  label?: string;
};

export type FlowParams = QueryParams & {
  include_timerange?: boolean | string;
  timerange?: string;
};

export type FlowSegmentParams = PagingParams & {
  accept_get_urls?: string | readonly string[];
  accept_storage_ids?: string | readonly string[];
  include_object_timerange?: boolean | string;
  object_id?: string;
  presigned?: boolean | string;
  reverse_order?: boolean | string;
  timerange?: string;
  verbose_storage?: boolean | string;
};

export type ObjectParams = PagingParams & {
  accept_get_urls?: string | readonly string[];
  accept_storage_ids?: string | readonly string[];
  presigned?: boolean | string;
  verbose_storage?: boolean | string;
};

type PagedObjectParams = ObjectParams &
  (
    | { limit: string | number; page?: string }
    | { limit?: string | number; page: string }
  );

export type WebhookListParams = PagingParams;

export type DeletionRequestListParams = PagingParams & {
  reverse_order?: boolean | string;
  sort_by?: "created" | "expiry";
};

export class TamossApiClient extends ApiTransport {
  async getService(): Promise<ServiceInfo> {
    return this.request<ServiceInfo>("/service");
  }

  async getHealth(): Promise<string> {
    return this.requestText("/healthz");
  }

  async getStorageBackends(): Promise<StorageBackend[]> {
    return this.request<StorageBackend[]>("/service/storage-backends");
  }

  async getSources(
    params?: SourceListParams,
    options?: ApiRequestOptions,
  ): Promise<PaginatedResponse<Source>> {
    return this.requestPaginated<Source>("/sources", params, options);
  }

  async getSource(sourceId: string): Promise<Source> {
    return this.request<Source>(path`/sources/${sourceId}`);
  }

  async getProfiles(
    params?: ProfileListParams,
    options?: ApiRequestOptions,
  ): Promise<PaginatedResponse<Profile>> {
    return this.requestPaginated<Profile>("/service/profiles", params, options);
  }

  async getProfile(profileId: string): Promise<Profile> {
    return this.request<Profile>(path`/service/profiles/${profileId}`);
  }

  async getFlows(
    params?: FlowListParams,
    options?: ApiRequestOptions,
  ): Promise<PaginatedResponse<Flow>> {
    return this.requestPaginated<Flow>("/flows", params, options);
  }

  async getFlow(
    flowId: string,
    params?: FlowParams,
    options: ApiRequestOptions = {},
  ): Promise<Flow> {
    return this.request<Flow>(path`/flows/${flowId}`, options, params);
  }

  async getFlowCollection(
    flowId: string,
    options: ApiRequestOptions = {},
  ): Promise<FlowCollectionItem[]> {
    return this.request<FlowCollectionItem[]>(
      path`/flows/${flowId}/flow_collection`,
      options,
    );
  }

  async getFlowSegments(
    flowId: string,
    params?: FlowSegmentParams,
    options?: ApiRequestOptions,
  ): Promise<PaginatedResponse<FlowSegment>> {
    return this.requestPaginated<FlowSegment>(
      path`/flows/${flowId}/segments`,
      params,
      options,
    );
  }

  async getObject(
    objectId: string,
    params: PagedObjectParams,
  ): Promise<PaginatedResourceResponse<MediaObject>>;
  async getObject(
    objectId: string,
    params?: ObjectParams,
  ): Promise<MediaObject>;
  async getObject(
    objectId: string,
    params?: ObjectParams,
  ): Promise<MediaObject | PaginatedResourceResponse<MediaObject>> {
    if (params?.limit !== undefined || params?.page !== undefined) {
      return this.requestPaginatedResource<MediaObject>(
        path`/objects/${objectId}`,
        params,
      );
    }
    return this.request<MediaObject>(path`/objects/${objectId}`, {}, params);
  }

  async getWebhooks(
    params?: WebhookListParams,
  ): Promise<PaginatedResponse<WebhookDetail>> {
    return this.requestPaginated<WebhookDetail>("/service/webhooks", params);
  }

  async getDeletionRequests(
    params?: DeletionRequestListParams,
    options?: ApiRequestOptions,
  ): Promise<PaginatedResponse<DeletionRequest>> {
    return this.requestPaginated<DeletionRequest>(
      "/flow-delete-requests",
      params,
      options,
    );
  }
}
