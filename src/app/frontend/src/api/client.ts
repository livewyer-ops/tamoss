import type {
  Source,
  Flow,
  FlowCollectionItem,
  FlowSegment,
  MediaObject,
  ServiceInfo,
  UpdateServiceInfo,
  StorageBackend,
  DeletionRequest,
  WebhookDetail,
  WebhookWritePayload,
  PaginatedResponse,
  PaginatedResourceResponse,
  HttpRequest,
  StorageAllocation,
  StorageAllocationOptions,
  FlowSegmentWrite,
} from "@/types/tams";
import {
  ApiError,
  ApiTransport,
  type PagingParams,
  type QueryParams,
} from "@/api/transport";

export { ApiError } from "@/api/transport";

export type StorageBackendListParams = QueryParams & {
  reverse_order?: boolean | string;
};

export type SourceListParams = PagingParams & {
  reverse_order?: boolean | string;
  sort_by?: "created" | "updated" | "label";
};

export type FlowListParams = PagingParams & {
  reverse_order?: boolean | string;
  sort_by?: "created" | "metadata_updated" | "label";
  source_id?: string;
  timerange?: string;
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

export type WebhookListParams = PagingParams & {
  reverse_order?: boolean | string;
};

export type DeletionRequestListParams = QueryParams & {
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

  async getRootPaths(): Promise<string[]> {
    return this.request<string[]>("/");
  }

  async updateServiceInfo(payload: UpdateServiceInfo): Promise<void> {
    return this.request<void>("/service", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  }

  async getStorageBackends(
    params?: StorageBackendListParams,
  ): Promise<StorageBackend[]> {
    return this.request<StorageBackend[]>(
      "/service/storage-backends",
      {},
      params,
    );
  }

  async getSources(
    params?: SourceListParams,
  ): Promise<PaginatedResponse<Source>> {
    return this.requestPaginated<Source>("/sources", params);
  }

  async getSource(sourceId: string): Promise<Source> {
    return this.request<Source>(`/sources/${sourceId}`);
  }

  async getSourceTags(
    sourceId: string,
  ): Promise<Record<string, string | string[]>> {
    return this.request<Record<string, string | string[]>>(
      `/sources/${sourceId}/tags`,
    );
  }

  async updateSourceTag(
    sourceId: string,
    name: string,
    value: string,
  ): Promise<void> {
    return this.request<void>(
      `/sources/${sourceId}/tags/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify(value),
      },
    );
  }

  async deleteSourceTag(sourceId: string, name: string): Promise<void> {
    return this.request<void>(
      `/sources/${sourceId}/tags/${encodeURIComponent(name)}`,
      {
        method: "DELETE",
      },
    );
  }

  async updateSourceLabel(sourceId: string, label: string): Promise<void> {
    return this.request<void>(`/sources/${sourceId}/label`, {
      method: "PUT",
      body: JSON.stringify(label),
    });
  }

  async updateSourceDescription(
    sourceId: string,
    description: string,
  ): Promise<void> {
    return this.request<void>(`/sources/${sourceId}/description`, {
      method: "PUT",
      body: JSON.stringify(description),
    });
  }

  async getFlows(params?: FlowListParams): Promise<PaginatedResponse<Flow>> {
    return this.requestPaginated<Flow>("/flows", params);
  }

  async getFlow(flowId: string, params?: FlowParams): Promise<Flow> {
    return this.request<Flow>(`/flows/${flowId}`, {}, params);
  }

  async updateFlow(flowId: string, data: Partial<Flow>): Promise<Flow> {
    return this.request<Flow>(`/flows/${flowId}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async deleteFlow(flowId: string): Promise<DeletionRequest | undefined> {
    return this.request<DeletionRequest | undefined>(`/flows/${flowId}`, {
      method: "DELETE",
    });
  }

  async getFlowTags(
    flowId: string,
  ): Promise<Record<string, string | string[]>> {
    return this.request<Record<string, string | string[]>>(
      `/flows/${flowId}/tags`,
    );
  }

  async updateFlowTag(
    flowId: string,
    name: string,
    value: string,
  ): Promise<void> {
    return this.request<void>(
      `/flows/${flowId}/tags/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        body: JSON.stringify(value),
      },
    );
  }

  async deleteFlowTag(flowId: string, name: string): Promise<void> {
    return this.request<void>(
      `/flows/${flowId}/tags/${encodeURIComponent(name)}`,
      {
        method: "DELETE",
      },
    );
  }

  async updateFlowLabel(flowId: string, label: string): Promise<void> {
    return this.request<void>(`/flows/${flowId}/label`, {
      method: "PUT",
      body: JSON.stringify(label),
    });
  }

  async updateFlowDescription(
    flowId: string,
    description: string,
  ): Promise<void> {
    return this.request<void>(`/flows/${flowId}/description`, {
      method: "PUT",
      body: JSON.stringify(description),
    });
  }

  async updateFlowAvgBitRate(
    flowId: string,
    avgBitRate: number,
  ): Promise<void> {
    return this.request<void>(`/flows/${flowId}/avg_bit_rate`, {
      method: "PUT",
      body: JSON.stringify(avgBitRate),
    });
  }

  async updateFlowMaxBitRate(
    flowId: string,
    maxBitRate: number,
  ): Promise<void> {
    return this.request<void>(`/flows/${flowId}/max_bit_rate`, {
      method: "PUT",
      body: JSON.stringify(maxBitRate),
    });
  }

  async getFlowCollection(flowId: string): Promise<FlowCollectionItem[]> {
    return this.request<FlowCollectionItem[]>(
      `/flows/${flowId}/flow_collection`,
    );
  }

  async setFlowCollection(
    flowId: string,
    items: Array<{ id: string; role: string }>,
  ): Promise<void> {
    return this.request<void>(`/flows/${flowId}/flow_collection`, {
      method: "PUT",
      body: JSON.stringify(items),
    });
  }

  async setFlowReadOnly(flowId: string, readOnly: boolean): Promise<void> {
    return this.request<void>(`/flows/${flowId}/read_only`, {
      method: "PUT",
      body: JSON.stringify(readOnly),
    });
  }

  async getFlowSegments(
    flowId: string,
    params?: FlowSegmentParams,
  ): Promise<PaginatedResponse<FlowSegment>> {
    return this.requestPaginated<FlowSegment>(
      `/flows/${flowId}/segments`,
      params,
    );
  }

  async deleteFlowSegments(
    flowId: string,
    timerange: string,
  ): Promise<DeletionRequest | undefined> {
    return this.request<DeletionRequest | undefined>(
      `/flows/${flowId}/segments`,
      { method: "DELETE" },
      { timerange },
    );
  }

  // Convenience wrapper around BBC PUT /flows/{flowId}.
  async createFlow(flowId: string, data: Partial<Flow>): Promise<Flow> {
    return this.request<Flow>(`/flows/${flowId}`, {
      method: "PUT",
      body: JSON.stringify(data),
    });
  }

  async addFlowSegments(
    flowId: string,
    segments: FlowSegmentWrite[],
  ): Promise<void> {
    return this.request<void>(`/flows/${flowId}/segments`, {
      method: "POST",
      body: JSON.stringify(segments),
    });
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
        `/objects/${objectId}`,
        params,
      );
    }
    return this.request<MediaObject>(`/objects/${objectId}`, {}, params);
  }

  async deleteObjectInstance(
    objectId: string,
    params: { storage_id?: string; label?: string },
  ): Promise<void> {
    return this.request<void>(
      `/objects/${encodeURIComponent(objectId)}/instances`,
      { method: "DELETE" },
      params,
    );
  }

  async getWebhooks(
    params?: WebhookListParams,
  ): Promise<PaginatedResponse<WebhookDetail>> {
    return this.requestPaginated<WebhookDetail>("/service/webhooks", params);
  }

  async getWebhook(webhookId: string): Promise<WebhookDetail> {
    return this.request<WebhookDetail>(`/service/webhooks/${webhookId}`);
  }

  async createWebhook(payload: WebhookWritePayload): Promise<WebhookDetail> {
    return this.request<WebhookDetail>("/service/webhooks", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  }

  async updateWebhook(
    webhookId: string,
    payload: Partial<WebhookWritePayload>,
  ): Promise<WebhookDetail> {
    return this.request<WebhookDetail>(`/service/webhooks/${webhookId}`, {
      method: "PUT",
      body: JSON.stringify(payload),
    });
  }

  async deleteWebhook(webhookId: string): Promise<void> {
    return this.request<void>(`/service/webhooks/${webhookId}`, {
      method: "DELETE",
    });
  }

  async getDeletionRequests(
    params?: DeletionRequestListParams,
  ): Promise<DeletionRequest[]> {
    return this.request<DeletionRequest[]>("/flow-delete-requests", {}, params);
  }

  async getDeletionRequest(requestId: string): Promise<DeletionRequest> {
    return this.request<DeletionRequest>(`/flow-delete-requests/${requestId}`);
  }

  async allocateStorage(
    flowId: string,
    objectIds: string[],
    options: StorageAllocationOptions = {},
  ): Promise<StorageAllocation> {
    const body = {
      object_ids: objectIds,
      ...(options.storageId ? { storage_id: options.storageId } : {}),
    };
    return this.request<StorageAllocation>(`/flows/${flowId}/storage`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  async allocateStorageByCount(
    flowId: string,
    limit: number,
    options: StorageAllocationOptions = {},
  ): Promise<StorageAllocation> {
    const body = {
      limit,
      ...(options.storageId ? { storage_id: options.storageId } : {}),
    };
    return this.request<StorageAllocation>(`/flows/${flowId}/storage`, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  // Raw upload (presigned URL, no auth headers)
  async uploadRaw(request: HttpRequest, media: Blob): Promise<void> {
    const headers = new Headers(request.headers);
    if (request["content-type"]) {
      headers.set("Content-Type", request["content-type"]);
    }
    const uploadBody =
      request.body && request.body.length > 0 ? request.body : media;
    const response = await fetch(request.url, {
      method: "PUT",
      body: uploadBody,
      credentials: "same-origin",
      headers,
    });
    if (!response.ok) throw new ApiError(response.status, "Upload failed");
  }
}
