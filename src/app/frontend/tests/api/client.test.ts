import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, TamossApiClient } from "@/api/client";

const mockFetch = vi.fn();
global.fetch = mockFetch;

function createClient() {
  return new TamossApiClient("https://api.example.com");
}

function lastCalledUrl(): URL {
  const lastCall = mockFetch.mock.calls[mockFetch.mock.calls.length - 1];
  return new URL(lastCall[0] as string);
}

function mockResponse(
  data: unknown,
  status = 200,
  headers: Record<string, string> = {},
) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(data),
    text: () => Promise.resolve(JSON.stringify(data)),
    headers: new Headers(headers),
  };
}

describe("TamossApiClient", () => {
  beforeEach(() => {
    mockFetch.mockReset();
  });

  it.each(["", []] as const)(
    "preserves explicit empty TAMS filters (%j)",
    async (empty) => {
      mockFetch.mockResolvedValue(mockResponse([]));
      const client = createClient();
      for (const list of [
        client.getSources.bind(client),
        client.getFlows.bind(client),
      ]) {
        await list({
          collected_by_ids: empty,
          label: null,
          page: undefined,
          limit: 50,
        });
        expect(lastCalledUrl().searchParams.get("collected_by_ids")).toBe("");
        expect(lastCalledUrl().searchParams.has("label")).toBe(false);
        expect(lastCalledUrl().searchParams.has("page")).toBe(false);
        expect(lastCalledUrl().searchParams.get("limit")).toBe("50");
        await list();
        expect(lastCalledUrl().searchParams.has("collected_by_ids")).toBe(
          false,
        );
      }
      for (const get of [
        (params: {
          accept_get_urls?: string | readonly string[];
          accept_storage_ids?: string | readonly string[];
          presigned?: boolean;
        }) => client.getFlowSegments("flow-1", params),
        (params: {
          accept_get_urls?: string | readonly string[];
          accept_storage_ids?: string | readonly string[];
          presigned?: boolean;
        }) => client.getObject("object/1", params),
      ]) {
        await get({
          accept_get_urls: empty,
          accept_storage_ids: empty,
          presigned: false,
        });
        expect(lastCalledUrl().searchParams.get("accept_get_urls")).toBe("");
        expect(lastCalledUrl().searchParams.get("accept_storage_ids")).toBe("");
        expect(lastCalledUrl().searchParams.get("presigned")).toBe("false");
        await get({});
        expect(lastCalledUrl().searchParams.has("accept_get_urls")).toBe(false);
        expect(lastCalledUrl().searchParams.has("accept_storage_ids")).toBe(
          false,
        );
      }
    },
  );

  describe("getService", () => {
    it("fetches service information", async () => {
      const serviceData = { name: "Test TAMS", api_version: "8.2" };
      mockFetch.mockResolvedValueOnce(mockResponse(serviceData));

      const client = createClient();
      const result = await client.getService();

      expect(result).toEqual(serviceData);
      expect(mockFetch).toHaveBeenCalledWith(
        "https://api.example.com/service",
        expect.objectContaining({
          headers: expect.objectContaining({
            "Content-Type": "application/json",
          }),
        }),
      );
    });

    it("resolves relative API bases against the current origin", async () => {
      const serviceData = { name: "Test TAMS", api_version: "8.2" };
      mockFetch.mockResolvedValueOnce(mockResponse(serviceData));

      const client = new TamossApiClient("/api");
      await client.getService();

      expect(mockFetch).toHaveBeenCalledWith(
        new URL("/api/service", window.location.origin).toString(),
        expect.any(Object),
      );
    });
  });

  describe("getSources", () => {
    it("fetches sources with pagination headers", async () => {
      const sources = [{ id: "source-1", label: "Test Source" }];
      const controller = new AbortController();
      const responseHeaders = new Headers({
        "X-Paging-NextKey": "next-page-key",
        "X-Paging-Limit": "50",
      });
      mockFetch.mockResolvedValueOnce(
        mockResponse(sources, 200, Object.fromEntries(responseHeaders)),
      );

      const client = createClient();
      const result = await client.getSources(
        { limit: "50" },
        { signal: controller.signal },
      );

      expect(result.data).toEqual(sources);
      expect(result.nextKey).toBe("next-page-key");
      expect(result.limit).toBe(50);
      expect(mockFetch.mock.calls[0][1]).toEqual(
        expect.objectContaining({ signal: controller.signal }),
      );
    });
  });

  describe("profiles", () => {
    it("fetches filtered pages and individual Profiles", async () => {
      const profile = {
        id: "00000000-0000-4000-8000-000000000101",
        flow_metadata: { format: "urn:x-nmos:format:video" },
      };
      const controller = new AbortController();
      mockFetch
        .mockResolvedValueOnce(
          mockResponse([profile], 200, {
            "X-Paging-NextKey": "next-profile-page",
          }),
        )
        .mockResolvedValueOnce(mockResponse(profile));

      const client = createClient();
      const page = await client.getProfiles(
        {
          codec: "video/h264",
          format: "urn:x-nmos:format:video",
          label: "HD production video",
          limit: 25,
        },
        { signal: controller.signal },
      );
      const listedUrl = lastCalledUrl();
      const item = await client.getProfile(profile.id);

      expect(page).toEqual({ data: [profile], nextKey: "next-profile-page" });
      expect(listedUrl.pathname).toBe("/service/profiles");
      expect(listedUrl.searchParams.get("codec")).toBe("video/h264");
      expect(listedUrl.searchParams.get("format")).toBe(
        "urn:x-nmos:format:video",
      );
      expect(listedUrl.searchParams.get("label")).toBe("HD production video");
      expect(item).toEqual(profile);
      expect(lastCalledUrl().pathname).toBe(
        "/service/profiles/00000000-0000-4000-8000-000000000101",
      );
    });
  });

  describe("getFlows", () => {
    it("serializes BBC flow discovery params", async () => {
      const controller = new AbortController();
      mockFetch.mockResolvedValueOnce(mockResponse([]));

      const client = createClient();
      await client.getFlows(
        {
          limit: 25,
          page: "next-page",
          source_id: "source-1",
          timerange: "[0:0_10:0)",
        },
        { signal: controller.signal },
      );

      const url = lastCalledUrl();
      expect(url.pathname).toBe("/flows");
      expect(url.searchParams.get("limit")).toBe("25");
      expect(url.searchParams.get("page")).toBe("next-page");
      expect(url.searchParams.get("source_id")).toBe("source-1");
      expect(url.searchParams.get("timerange")).toBe("[0:0_10:0)");
      expect(mockFetch.mock.calls[0][1]).toEqual(
        expect.objectContaining({ signal: controller.signal }),
      );
    });
  });

  describe("getFlow", () => {
    it("fetches a single flow", async () => {
      const flow = {
        id: "flow-1",
        source_id: "source-1",
        codec: "video/h264",
      };
      mockFetch.mockResolvedValueOnce(mockResponse(flow));

      const client = createClient();
      const result = await client.getFlow("flow-1");

      expect(result).toEqual(flow);
      expect(mockFetch).toHaveBeenCalledWith(
        "https://api.example.com/flows/flow-1",
        expect.any(Object),
      );
    });

    it("serializes single-flow include_timerange params", async () => {
      const flow = { id: "flow-1", source_id: "source-1" };
      const controller = new AbortController();
      mockFetch.mockResolvedValueOnce(mockResponse(flow));

      const client = createClient();
      await client.getFlow(
        "flow-1",
        {
          include_timerange: true,
          timerange: "[0:0_10:0)",
        },
        { signal: controller.signal },
      );

      const url = lastCalledUrl();
      expect(url.pathname).toBe("/flows/flow-1");
      expect(url.searchParams.get("include_timerange")).toBe("true");
      expect(url.searchParams.get("timerange")).toBe("[0:0_10:0)");
      expect(mockFetch.mock.calls[0][1]).toEqual(
        expect.objectContaining({ signal: controller.signal }),
      );
    });
  });

  describe("getObject", () => {
    it("encodes reserved characters in BBC TAMS Object IDs", async () => {
      mockFetch.mockResolvedValueOnce(
        mockResponse({ id: "folder/clip #1.ts" }),
      );

      const client = createClient();
      await client.getObject("folder/clip #1.ts");

      expect(lastCalledUrl().pathname).toBe("/objects/folder%2Fclip%20%231.ts");
    });
  });

  describe("error handling", () => {
    it("throws ApiError on non-OK response", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 404,
        text: () => Promise.resolve("Not found"),
      });

      const client = createClient();
      await expect(client.getFlow("nonexistent")).rejects.toThrow(ApiError);
    });

    it("includes status code in ApiError", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        text: () => Promise.resolve("Unauthorized"),
      });

      const client = createClient();
      try {
        await client.getService();
        expect.fail("Should have thrown");
      } catch (err) {
        expect(err).toBeInstanceOf(ApiError);
        expect((err as ApiError).status).toBe(401);
      }
    });

    it("uses JSON detail as the ApiError message", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 409,
        text: () =>
          Promise.resolve(
            JSON.stringify({ detail: "Segment overlaps existing timerange" }),
          ),
      });

      const client = createClient();
      await expect(client.getFlowSegments("flow-1")).rejects.toMatchObject({
        message: "Segment overlaps existing timerange",
        status: 409,
      });
    });

    it("prefers TAMOSS error summaries over detail and raw JSON", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 400,
        text: () =>
          Promise.resolve(
            JSON.stringify({
              type: "bad_request",
              summary: "Invalid Flow Segment JSON.",
              detail: "Raw backend detail",
            }),
          ),
      });

      const client = createClient();
      await expect(client.getFlowSegments("flow-1")).rejects.toMatchObject({
        message: "Invalid Flow Segment JSON.",
        status: 400,
      });
    });

    it("uses validation detail messages when the backend returns an array", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 422,
        text: () =>
          Promise.resolve(
            JSON.stringify({
              detail: [{ msg: "Input should be a valid timerange" }],
            }),
          ),
      });

      const client = createClient();
      await expect(client.getFlowSegments("flow-1")).rejects.toMatchObject({
        message: "Input should be a valid timerange",
        status: 422,
      });
    });
  });

  describe("authorization boundary", () => {
    it("leaves authentication to the same-origin reverse proxy", async () => {
      const serviceData = { name: "Test TAMS", api_version: "8.2" };
      mockFetch.mockResolvedValueOnce(mockResponse(serviceData));

      const client = new TamossApiClient("/api");
      await client.getService();

      const calledUrl = mockFetch.mock.calls[0][0];
      const calledOptions = mockFetch.mock.calls[0][1];
      expect(calledUrl).not.toContain("access_token");
      expect(calledOptions.headers).not.toHaveProperty("Authorization");
    });
  });

  describe("getFlowSegments", () => {
    it("serializes playback segment discovery params", async () => {
      const controller = new AbortController();
      mockFetch.mockResolvedValueOnce(mockResponse([]));

      const client = createClient();
      await client.getFlowSegments(
        "flow-1",
        {
          accept_get_urls: ["preview", "external"],
          accept_storage_ids: ["storage-a", "storage-b"],
          include_object_timerange: true,
          limit: 100,
          object_id: "object-1",
          page: "page-2",
          presigned: true,
          reverse_order: false,
          timerange: "[0:0_60:0)",
          verbose_storage: true,
        },
        { signal: controller.signal },
      );

      const url = lastCalledUrl();
      expect(url.pathname).toBe("/flows/flow-1/segments");
      expect(url.searchParams.get("accept_get_urls")).toBe("preview,external");
      expect(url.searchParams.get("accept_storage_ids")).toBe(
        "storage-a,storage-b",
      );
      expect(url.searchParams.get("include_object_timerange")).toBe("true");
      expect(url.searchParams.get("limit")).toBe("100");
      expect(url.searchParams.get("object_id")).toBe("object-1");
      expect(url.searchParams.get("page")).toBe("page-2");
      expect(url.searchParams.get("presigned")).toBe("true");
      expect(url.searchParams.get("reverse_order")).toBe("false");
      expect(url.searchParams.get("timerange")).toBe("[0:0_60:0)");
      expect(url.searchParams.get("verbose_storage")).toBe("true");
      expect(mockFetch.mock.calls[0][1]).toEqual(
        expect.objectContaining({ signal: controller.signal }),
      );
    });
  });

  describe("getDeletionRequests", () => {
    it("preserves paging headers and listing parameters", async () => {
      const controller = new AbortController();
      const requests = [{ id: "delete-1", status: "started" }];
      mockFetch.mockResolvedValueOnce(
        mockResponse(requests, 200, {
          "X-Paging-NextKey": "next-delete-page",
          "X-Paging-Limit": "50",
        }),
      );

      const client = createClient();
      const result = await client.getDeletionRequests(
        {
          limit: 50,
          page: "current-page",
          sort_by: "created",
          reverse_order: true,
        },
        { signal: controller.signal },
      );

      expect(result).toEqual({
        data: requests,
        nextKey: "next-delete-page",
        limit: 50,
      });
      const url = lastCalledUrl();
      expect(url.pathname).toBe("/flow-delete-requests");
      expect(url.searchParams.get("page")).toBe("current-page");
      expect(url.searchParams.get("sort_by")).toBe("created");
      expect(url.searchParams.get("reverse_order")).toBe("true");
      expect(mockFetch.mock.calls[0][1]).toEqual(
        expect.objectContaining({ signal: controller.signal }),
      );
    });
  });

  describe("getObject", () => {
    it("serializes media object URL filtering params", async () => {
      const mediaObject = { id: "object-1", get_urls: [] };
      mockFetch.mockResolvedValueOnce(mockResponse(mediaObject));

      const client = createClient();
      await client.getObject("object-1", {
        accept_get_urls: "preview",
        accept_storage_ids: ["storage-a", "storage-b"],
        presigned: true,
        verbose_storage: false,
      });

      const url = lastCalledUrl();
      expect(url.pathname).toBe("/objects/object-1");
      expect(url.searchParams.get("accept_get_urls")).toBe("preview");
      expect(url.searchParams.get("accept_storage_ids")).toBe(
        "storage-a,storage-b",
      );
      expect(url.searchParams.get("presigned")).toBe("true");
      expect(url.searchParams.get("verbose_storage")).toBe("false");
    });

    it("preserves object paging headers when paging params are supplied", async () => {
      const mediaObject = { id: "object-1", referenced_by_flows: ["flow-1"] };
      mockFetch.mockResolvedValueOnce(
        mockResponse(mediaObject, 200, {
          "X-Paging-NextKey": "next-object-page",
          "X-Paging-Limit": "50",
        }),
      );

      const client = createClient();
      const result = await client.getObject("object-1", { limit: 50 });

      expect(result.data).toEqual(mediaObject);
      expect(result.nextKey).toBe("next-object-page");
      expect(result.limit).toBe(50);
      expect(lastCalledUrl().searchParams.get("limit")).toBe("50");
    });
  });

  describe("path segment encoding", () => {
    const traversal = "..%2F..%2Fservice";

    it.each([".", ".."])("rejects %s before sending a request", async (id) => {
      const client = new TamossApiClient("/api");
      await expect(client.getObject(id)).rejects.toThrow(
        "Invalid resource identifier.",
      );
      await expect(client.getFlow(id)).rejects.toThrow(
        "Invalid resource identifier.",
      );
      expect(mockFetch).not.toHaveBeenCalled();
    });

    it("keeps an identifier inside one path segment", async () => {
      const client = new TamossApiClient("/api");
      mockFetch.mockResolvedValue(mockResponse({}));

      await client.getFlow(traversal);
      await client.getSource(traversal);
      await client.getProfile(traversal);
      await client.getObject(traversal);

      const paths = mockFetch.mock.calls.map(
        (call) => new URL(call[0] as string).pathname,
      );
      expect(paths).toEqual([
        "/api/flows/..%252F..%252Fservice",
        "/api/sources/..%252F..%252Fservice",
        "/api/service/profiles/..%252F..%252Fservice",
        "/api/objects/..%252F..%252Fservice",
      ]);
    });

    it("does not let an unencoded identifier escape the read-only boundary", async () => {
      mockFetch.mockResolvedValueOnce(mockResponse({}));

      const client = new TamossApiClient("/api");
      await client.getFlow("../../service");

      expect(lastCalledUrl().pathname).toBe("/api/flows/..%2F..%2Fservice");
    });

    it("encodes a nested identifier exactly once", async () => {
      mockFetch.mockResolvedValue(mockResponse({}));

      const client = new TamossApiClient("/api");
      await client.getObject("bucket/key with space");
      expect(lastCalledUrl().pathname).toBe(
        "/api/objects/bucket%2Fkey%20with%20space",
      );

      await client.getFlowCollection("flow 1");
      expect(lastCalledUrl().pathname).toBe(
        "/api/flows/flow%201/flow_collection",
      );
    });
  });
});
