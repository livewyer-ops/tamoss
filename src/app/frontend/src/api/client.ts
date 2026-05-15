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
  FlowSegmentWrite,
} from "@/types/tams";

type QueryScalar = string | number | boolean;
type QueryParamValue = QueryScalar | readonly QueryScalar[];
export type QueryParams = Record<string, QueryParamValue | null | undefined>;

type PagingParams = QueryParams & {
  limit?: string | number;
  page?: string;
};

export type SourceListParams = PagingParams;

export type FlowListParams = PagingParams & {
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

export type WebhookListParams = PagingParams;

function errorMessageFromText(text: string): string {
  if (!text.trim()) return "Unknown error";
  try {
    const data = JSON.parse(text) as { detail?: unknown };
    if (typeof data.detail === "string") return data.detail;
    if (Array.isArray(data.detail)) {
      return data.detail
        .map((item) => {
          if (typeof item === "string") return item;
          if (
            item &&
            typeof item === "object" &&
            "msg" in item &&
            typeof item.msg === "string"
          ) {
            return item.msg;
          }
          return JSON.stringify(item);
        })
        .join("; ");
    }
  } catch {
    // Response was plain text.
  }
  return text;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export class TamossApiClient {
  private baseUrl: string;
  private token: string;

  constructor(baseUrl: string, token = "") {
    // Ensure trailing slash so URL() preserves the base path
    this.baseUrl = baseUrl.endsWith("/") ? baseUrl : baseUrl + "/";
    this.token = token;
  }

  private buildUrl(path: string, params?: QueryParams): string {
    const rel = path.startsWith("/") ? path.slice(1) : path;
    const baseUrl =
      this.baseUrl.startsWith("http://") || this.baseUrl.startsWith("https://")
        ? this.baseUrl
        : new URL(this.baseUrl, window.location.origin).toString();
    const url = new URL(rel, baseUrl);
    this.applyQueryParams(url, params);
    return url.toString();
  }

  private applyQueryParams(url: URL, params?: QueryParams): void {
    if (!params) return;
    for (const [key, value] of Object.entries(params)) {
      if (value === undefined || value === null) continue;
      const encodedValue = Array.isArray(value)
        ? value.map(String).join(",")
        : String(value);
      if (encodedValue.length > 0) {
        url.searchParams.set(key, encodedValue);
      }
    }
  }

  private authHeaders(): Record<string, string> {
    if (this.token) {
      return { Authorization: `Bearer ${this.token}` };
    }
    return {};
  }

  private async request<T>(
    path: string,
    options: RequestInit = {},
    params?: QueryParams,
  ): Promise<T> {
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      ...this.authHeaders(),
      ...(options.headers as Record<string, string>),
    };

    const response = await fetch(this.buildUrl(path, params), {
      ...options,
      headers,
    });

    if (!response.ok) {
      const text = await response.text().catch(() => "Unknown error");
      throw new ApiError(response.status, errorMessageFromText(text));
    }

    if (response.status === 204) {
      return undefined as T;
    }

    const text = await response.text();
    if (!text.trim()) {
      return undefined as T;
    }

    return JSON.parse(text) as T;
  }

  private async requestText(
    path: string,
    options: RequestInit = {},
  ): Promise<string> {
    const headers: Record<string, string> = {
      ...this.authHeaders(),
      ...(options.headers as Record<string, string>),
    };

    const response = await fetch(this.buildUrl(path), {
      ...options,
      headers,
    });

    if (!response.ok) {
      const text = await response.text().catch(() => "Unknown error");
      throw new ApiError(response.status, errorMessageFromText(text));
    }

    return response.text();
  }

  private async requestPaginated<T>(
    path: string,
    params?: QueryParams,
  ): Promise<PaginatedResponse<T>> {
    const response = await fetch(this.buildUrl(path, params), {
      headers: {
        "Content-Type": "application/json",
        ...this.authHeaders(),
      },
    });

    if (!response.ok) {
      const text = await response.text().catch(() => "Unknown error");
      throw new ApiError(response.status, errorMessageFromText(text));
    }

    const data: T[] = await response.json();
    const nextKey = response.headers.get("X-Paging-NextKey") ?? undefined;
    const limit = response.headers.get("X-Paging-Limit")
      ? parseInt(response.headers.get("X-Paging-Limit")!, 10)
      : undefined;

    return { data, nextKey, limit };
  }

  private async requestPaginatedResource<T>(
    path: string,
    params?: QueryParams,
  ): Promise<PaginatedResourceResponse<T>> {
    const response = await fetch(this.buildUrl(path, params), {
      headers: {
        "Content-Type": "application/json",
        ...this.authHeaders(),
      },
    });

    if (!response.ok) {
      const text = await response.text().catch(() => "Unknown error");
      throw new ApiError(response.status, errorMessageFromText(text));
    }

    const data: T = await response.json();
    const nextKey = response.headers.get("X-Paging-NextKey") ?? undefined;
    const limit = response.headers.get("X-Paging-Limit")
      ? parseInt(response.headers.get("X-Paging-Limit")!, 10)
      : undefined;

    return { data, nextKey, limit };
  }

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

  async getStorageBackends(): Promise<StorageBackend[]> {
    return this.request<StorageBackend[]>("/service/storage-backends");
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

  async getDeletionRequests(): Promise<DeletionRequest[]> {
    return this.request<DeletionRequest[]>("/flow-delete-requests");
  }

  async getDeletionRequest(requestId: string): Promise<DeletionRequest> {
    return this.request<DeletionRequest>(`/flow-delete-requests/${requestId}`);
  }

  async allocateStorage(
    flowId: string,
    objectIds: string[],
  ): Promise<StorageAllocation> {
    return this.request<StorageAllocation>(`/flows/${flowId}/storage`, {
      method: "POST",
      body: JSON.stringify({ object_ids: objectIds }),
    });
  }

  async allocateStorageByCount(
    flowId: string,
    limit: number,
  ): Promise<StorageAllocation> {
    return this.request<StorageAllocation>(`/flows/${flowId}/storage`, {
      method: "POST",
      body: JSON.stringify({ limit }),
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
