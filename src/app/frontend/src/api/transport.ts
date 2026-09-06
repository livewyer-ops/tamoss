import type {
  PaginatedResourceResponse,
  PaginatedResponse,
} from "@/types/tams";

type QueryScalar = string | number | boolean;
type QueryParamValue = QueryScalar | readonly QueryScalar[];
export type QueryParams = Record<string, QueryParamValue | null | undefined>;
type RequestHeaders = Record<string, string>;

export interface ApiRequestOptions {
  signal?: AbortSignal;
}

export type PagingParams = QueryParams & {
  limit?: string | number;
  page?: string;
};

/** Encode identifiers as path segments; reject dots that URL parsing resolves. */
export function path(
  literals: TemplateStringsArray,
  ...values: ReadonlyArray<string | number>
): string {
  let result = literals[0];
  for (let index = 0; index < values.length; index += 1) {
    if (values[index] === "." || values[index] === "..") {
      throw new Error("Invalid resource identifier.");
    }
    result += encodeURIComponent(values[index]) + literals[index + 1];
  }
  return result;
}

export function errorMessageFromText(text: string): string {
  if (!text.trim()) return "Service request failed";
  try {
    const data = JSON.parse(text) as { summary?: unknown; detail?: unknown };
    if (typeof data.summary === "string") return data.summary;
    const detailMessage = messageFromDetail(data.detail);
    if (detailMessage) return detailMessage;
  } catch {
    // Response was plain text.
  }
  return text;
}

function messageFromDetail(detail: unknown): string | null {
  if (typeof detail === "string") return detail;
  if (!Array.isArray(detail)) return null;
  return detail
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

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export class ApiTransport {
  private baseUrl: string;

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl.endsWith("/") ? baseUrl : `${baseUrl}/`;
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
      url.searchParams.set(key, encodedValue);
    }
  }

  protected async request<T>(
    path: string,
    options: RequestInit = {},
    params?: QueryParams,
  ): Promise<T> {
    const response = await this.fetchResponse(path, options, params);
    if (response.status === 204) {
      return undefined as T;
    }

    return this.readJson<T>(response);
  }

  protected async requestText(
    path: string,
    options: RequestInit = {},
  ): Promise<string> {
    const response = await this.fetchResponse(path, options, undefined, {
      jsonContent: false,
    });
    return response.text();
  }

  protected async requestPaginated<T>(
    path: string,
    params?: QueryParams,
    options: ApiRequestOptions = {},
  ): Promise<PaginatedResponse<T>> {
    const response = await this.fetchResponse(path, options, params);
    return {
      data: await this.readJson<T[]>(response),
      ...this.paging(response),
    };
  }

  protected async requestPaginatedResource<T>(
    path: string,
    params?: QueryParams,
  ): Promise<PaginatedResourceResponse<T>> {
    const response = await this.fetchResponse(path, {}, params);
    return { data: await this.readJson<T>(response), ...this.paging(response) };
  }

  private async fetchResponse(
    path: string,
    options: RequestInit = {},
    params?: QueryParams,
    config: { jsonContent?: boolean } = {},
  ): Promise<Response> {
    const jsonContent = config.jsonContent ?? true;
    const headers: RequestHeaders = {
      ...(jsonContent ? { "Content-Type": "application/json" } : {}),
      ...(options.headers as RequestHeaders),
    };

    const response = await fetch(this.buildUrl(path, params), {
      ...options,
      headers,
    });

    if (!response.ok) {
      const text = await response.text().catch(() => "Service request failed");
      throw new ApiError(response.status, errorMessageFromText(text));
    }

    return response;
  }

  private async readJson<T>(response: Response): Promise<T> {
    const text = await response.text();
    if (!text.trim()) {
      return undefined as T;
    }
    return JSON.parse(text) as T;
  }

  private paging(response: Response): {
    nextKey?: string;
    limit?: number;
  } {
    const nextKey = response.headers.get("X-Paging-NextKey") ?? undefined;
    const limitHeader = response.headers.get("X-Paging-Limit");
    const limit = limitHeader ? parseInt(limitHeader, 10) : undefined;
    return { nextKey, limit };
  }
}
