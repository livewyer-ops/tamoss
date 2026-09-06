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
    headers: new Map(Object.entries(headers)),
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
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: () => Promise.resolve(sources),
        headers: responseHeaders,
      });

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
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: () => Promise.resolve([]),
        headers: new Headers({}),
      });

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
      await expect(client.addFlowSegments("flow-1", [])).rejects.toMatchObject({
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
      await expect(client.addFlowSegments("flow-1", [])).rejects.toMatchObject({
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

  describe("deleteFlow", () => {
    it("sends DELETE request", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 204,
        json: () => Promise.resolve(undefined),
        text: () => Promise.resolve(""),
      });

      const client = createClient();
      await client.deleteFlow("flow-1");

      expect(mockFetch).toHaveBeenCalledWith(
        "https://api.example.com/flows/flow-1",
        expect.objectContaining({ method: "DELETE" }),
      );
    });

    it("returns a background deletion request body", async () => {
      const deletionRequest = {
        id: "delete-1",
        flow_id: "flow-1",
        timerange_to_delete: "[0:0_10:0)",
        delete_flow: true,
        status: "created",
      };
      mockFetch.mockResolvedValueOnce(mockResponse(deletionRequest, 202));

      const client = createClient();
      await expect(client.deleteFlow("flow-1")).resolves.toEqual(
        deletionRequest,
      );
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

  describe("updateFlowTag", () => {
    it("sends PUT request with encoded tag name", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 204,
        json: () => Promise.resolve(undefined),
        text: () => Promise.resolve(""),
      });

      const client = createClient();
      await client.updateFlowTag("flow-1", "my tag", "value");

      expect(mockFetch).toHaveBeenCalledWith(
        "https://api.example.com/flows/flow-1/tags/my%20tag",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify("value"),
        }),
      );
    });
  });

  describe("createFlow", () => {
    it("sends PUT request to /flows/{flowId} with flow data", async () => {
      const newFlow = {
        id: "new-flow-1",
        source_id: "source-1",
        format: "urn:x-nmos:format:video",
      };
      mockFetch.mockResolvedValueOnce(mockResponse(newFlow));

      const client = createClient();
      const result = await client.createFlow("new-flow-1", {
        source_id: "source-1",
        format: "urn:x-nmos:format:video",
        codec: "video/h264",
      });

      expect(result).toEqual(newFlow);
      expect(mockFetch).toHaveBeenCalledWith(
        "https://api.example.com/flows/new-flow-1",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({
            source_id: "source-1",
            format: "urn:x-nmos:format:video",
            codec: "video/h264",
          }),
        }),
      );
    });
  });

  describe("setFlowCollection", () => {
    it("sends PUT request with collection items", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 204,
        json: () => Promise.resolve(undefined),
        text: () => Promise.resolve(""),
      });

      const client = createClient();
      const items = [
        { id: "flow-video-1", role: "video" },
        { id: "flow-audio-1", role: "audio" },
      ];
      await client.setFlowCollection("flow-multi-1", items);

      expect(mockFetch).toHaveBeenCalledWith(
        "https://api.example.com/flows/flow-multi-1/flow_collection",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify(items),
        }),
      );
    });
  });

  describe("addFlowSegments", () => {
    it("sends POST request with segments array and accepts an empty 201 response", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 201,
        json: () =>
          Promise.reject(new Error("empty body should not be parsed as JSON")),
        text: () => Promise.resolve(""),
      });

      const client = createClient();
      const segs = [
        {
          object_id: "obj-1",
          timerange: "[0:0_6:0)",
          object_timerange: "[10:0_16:0)",
        },
        { object_id: "obj-2", timerange: "[6:0_12:0)" },
      ];
      await client.addFlowSegments("flow-1", segs);

      expect(mockFetch).toHaveBeenCalledWith(
        "https://api.example.com/flows/flow-1/segments",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify(segs),
        }),
      );
    });
  });

  describe("getFlowSegments", () => {
    it("serializes playback segment discovery params", async () => {
      const controller = new AbortController();
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: () => Promise.resolve([]),
        headers: new Headers({}),
      });

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

  describe("deleteFlowSegments", () => {
    it("uses query serialization for timerange deletion", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 202,
        text: () => Promise.resolve(""),
      });

      const client = createClient();
      await client.deleteFlowSegments("flow-1", "[0:0_10:0)");

      const url = lastCalledUrl();
      expect(url.pathname).toBe("/flows/flow-1/segments");
      expect(url.searchParams.get("timerange")).toBe("[0:0_10:0)");
      expect(mockFetch.mock.calls[0][1]).toEqual(
        expect.objectContaining({ method: "DELETE" }),
      );
    });

    it("returns a background segment deletion request body", async () => {
      const deletionRequest = {
        id: "delete-2",
        flow_id: "flow-1",
        timerange_to_delete: "[0:0_10:0)",
        delete_flow: false,
        status: "created",
      };
      mockFetch.mockResolvedValueOnce(mockResponse(deletionRequest, 202));

      const client = createClient();
      await expect(
        client.deleteFlowSegments("flow-1", "[0:0_10:0)"),
      ).resolves.toEqual(deletionRequest);
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

  describe("deleteObjectInstance", () => {
    it("serializes storage instance deletion params", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 204,
        text: () => Promise.resolve(""),
      });

      const client = createClient();
      await client.deleteObjectInstance("object-1.ts", {
        storage_id: "storage-1",
      });

      const url = lastCalledUrl();
      expect(url.pathname).toBe("/objects/object-1.ts/instances");
      expect(url.searchParams.get("storage_id")).toBe("storage-1");
      expect(mockFetch.mock.calls[0][1]).toEqual(
        expect.objectContaining({ method: "DELETE" }),
      );
    });
  });

  describe("allocateStorage", () => {
    it("returns storage allocation request headers", async () => {
      const allocation = {
        media_objects: [
          {
            object_id: "obj-1",
            put_url: {
              url: "https://upload.example/obj-1",
              headers: { "Content-Type": "video/mp2t" },
            },
          },
        ],
      };
      mockFetch.mockResolvedValueOnce(mockResponse(allocation));

      const client = createClient();
      const result = await client.allocateStorage("flow-1", ["obj-1"]);

      expect(result).toEqual(allocation);
      expect(mockFetch).toHaveBeenCalledWith(
        "https://api.example.com/flows/flow-1/storage",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ object_ids: ["obj-1"] }),
        }),
      );
    });

    it("sends selected storage backend on allocation requests", async () => {
      mockFetch.mockResolvedValueOnce(mockResponse({ media_objects: [] }));

      const client = createClient();
      await client.allocateStorage("flow-1", ["obj-1"], {
        storageId: "storage-2",
      });

      expect(mockFetch).toHaveBeenCalledWith(
        "https://api.example.com/flows/flow-1/storage",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            object_ids: ["obj-1"],
            storage_id: "storage-2",
          }),
        }),
      );
    });

    it("sends selected storage backend on count allocation requests", async () => {
      mockFetch.mockResolvedValueOnce(mockResponse({ media_objects: [] }));

      const client = createClient();
      await client.allocateStorageByCount("flow-1", 2, {
        storageId: "storage-2",
      });

      expect(mockFetch).toHaveBeenCalledWith(
        "https://api.example.com/flows/flow-1/storage",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ limit: 2, storage_id: "storage-2" }),
        }),
      );
    });
  });

  describe("createWebhook", () => {
    it("sends storage backend URL preferences", async () => {
      mockFetch.mockResolvedValueOnce(mockResponse({ id: "webhook-1" }));

      const client = createClient();
      await client.createWebhook({
        url: "https://hooks.example.test/tams",
        events: ["flows/created"],
        accept_storage_ids: ["storage-1", "storage-2"],
      });

      expect(mockFetch).toHaveBeenCalledWith(
        "https://api.example.com/service/webhooks",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            url: "https://hooks.example.test/tams",
            events: ["flows/created"],
            accept_storage_ids: ["storage-1", "storage-2"],
          }),
        }),
      );
    });
  });

  describe("uploadRaw", () => {
    it("uses the returned upload request headers", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
      });

      const body = new Blob(["media"], { type: "video/mp2t" });
      const client = createClient();
      await client.uploadRaw(
        {
          url: "https://upload.example/obj-1",
          headers: { "Content-Type": "video/mp2t", "x-amz-meta-test": "1" },
        },
        body,
      );

      expect(mockFetch).toHaveBeenCalledWith(
        "https://upload.example/obj-1",
        expect.objectContaining({
          method: "PUT",
          body,
          credentials: "same-origin",
          headers: expect.any(Headers),
        }),
      );
      const headers = mockFetch.mock.calls[0][1].headers as Headers;
      expect(headers.get("Content-Type")).toBe("video/mp2t");
      expect(headers.get("x-amz-meta-test")).toBe("1");
    });

    it("uses content-type when headers are omitted", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
      });

      const client = createClient();
      await client.uploadRaw(
        { url: "https://upload.example/obj-1", "content-type": "audio/wav" },
        new Blob(["media"]),
      );

      const headers = mockFetch.mock.calls[0][1].headers as Headers;
      expect(headers.get("Content-Type")).toBe("audio/wav");
    });

    it("lets content-type override conflicting headers", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
      });

      const client = createClient();
      await client.uploadRaw(
        {
          url: "https://upload.example/obj-1",
          "content-type": "video/mp2t",
          headers: { "Content-Type": "video/mp4" },
        },
        new Blob(["media"]),
      );

      const headers = mockFetch.mock.calls[0][1].headers as Headers;
      expect(headers.get("Content-Type")).toBe("video/mp2t");
    });

    it("uses the media blob when request body is empty", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
      });

      const media = new Blob(["media"]);
      const client = createClient();
      await client.uploadRaw(
        { url: "https://upload.example/obj-1", body: "" },
        media,
      );

      expect(mockFetch).toHaveBeenCalledWith(
        "https://upload.example/obj-1",
        expect.objectContaining({ body: media }),
      );
    });

    it("uses a non-empty request body when supplied", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
      });

      const client = createClient();
      await client.uploadRaw(
        { url: "https://upload.example/obj-1", body: "signed-body" },
        new Blob(["media"]),
      );

      expect(mockFetch).toHaveBeenCalledWith(
        "https://upload.example/obj-1",
        expect.objectContaining({ body: "signed-body" }),
      );
    });
  });

  describe("path segment encoding", () => {
    const traversal = "..%2F..%2Fservice";

    it("keeps an identifier inside one path segment", async () => {
      const client = new TamossApiClient("/api");
      mockFetch.mockResolvedValue(mockResponse({}));

      await client.getFlow(traversal);
      await client.getSource(traversal);
      await client.getWebhook(traversal);
      await client.getDeletionRequest(traversal);

      const paths = mockFetch.mock.calls.map(
        (call) => new URL(call[0] as string).pathname,
      );
      expect(paths).toEqual([
        "/api/flows/..%252F..%252Fservice",
        "/api/sources/..%252F..%252Fservice",
        "/api/service/webhooks/..%252F..%252Fservice",
        "/api/flow-delete-requests/..%252F..%252Fservice",
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

      await client.getFlowTags("flow 1");
      expect(lastCalledUrl().pathname).toBe("/api/flows/flow%201/tags");
    });
  });
});
