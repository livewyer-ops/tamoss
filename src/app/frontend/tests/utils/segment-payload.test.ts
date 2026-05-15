import { describe, expect, it } from "vitest";
import { segmentToWritePayload } from "@/utils/segment-payload";

describe("segmentToWritePayload", () => {
  it("preserves segment timing metadata for object reuse", () => {
    expect(
      segmentToWritePayload({
        object_id: "object-1",
        timerange: "[10:0_16:0)",
        ts_offset: "5:0",
        object_timerange: "[5:0_11:0)",
        last_duration: "1:25",
        sample_offset: 10,
        sample_count: 150,
        key_frame_count: 1,
      }),
    ).toEqual({
      object_id: "object-1",
      timerange: "[10:0_16:0)",
      ts_offset: "5:0",
      object_timerange: "[5:0_11:0)",
      last_duration: "1:25",
      sample_offset: 10,
      sample_count: 150,
      key_frame_count: 1,
    });
  });

  it("posts only uncontrolled external get_urls", () => {
    expect(
      segmentToWritePayload({
        object_id: "external-object",
        timerange: "[0:0_6:0)",
        get_urls: [
          { url: "https://controlled.example/object.ts", controlled: true },
          {
            url: "https://external.example/object.ts",
            controlled: false,
            label: "external",
            storage_id: "ignored-storage-id",
          },
        ],
      }),
    ).toEqual({
      object_id: "external-object",
      timerange: "[0:0_6:0)",
      get_urls: [
        { url: "https://external.example/object.ts", label: "external" },
      ],
    });
  });

  it("treats missing controlled as service-controlled", () => {
    expect(
      segmentToWritePayload({
        object_id: "object-1",
        timerange: "[0:0_6:0)",
        get_urls: [
          { url: "https://service.example/object.ts", label: "service" },
        ],
      }),
    ).toEqual({
      object_id: "object-1",
      timerange: "[0:0_6:0)",
    });
  });

  it("omits uncontrolled get_urls without labels", () => {
    expect(
      segmentToWritePayload({
        object_id: "object-1",
        timerange: "[0:0_6:0)",
        get_urls: [
          { url: "https://external.example/object.ts", controlled: false },
        ],
      }),
    ).toEqual({
      object_id: "object-1",
      timerange: "[0:0_6:0)",
    });
  });

  it("omits empty get_urls", () => {
    expect(
      segmentToWritePayload({
        object_id: "object-1",
        timerange: "[0:0_6:0)",
        get_urls: [],
      }),
    ).toEqual({
      object_id: "object-1",
      timerange: "[0:0_6:0)",
    });
  });
});
