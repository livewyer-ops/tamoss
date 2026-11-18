import { describe, expect, it } from "vitest";
import {
  formatBitRate,
  formatCodec,
  formatDate,
  formatFormat,
  formatFrameRate,
  formatResolution,
  formatTimerange,
} from "@/utils/format";

describe("formatTimerange", () => {
  it("returns 'All time' for undefined or '_'", () => {
    expect(formatTimerange(undefined)).toBe("All time");
    expect(formatTimerange("_")).toBe("All time");
  });

  it("returns 'Empty' for empty range", () => {
    expect(formatTimerange("()")).toBe("Empty");
  });

  it("returns the timerange string as-is", () => {
    expect(formatTimerange("[0:0_10:0)")).toBe("[0:0_10:0)");
  });
});

describe("formatDate", () => {
  it("returns 'N/A' for undefined", () => {
    expect(formatDate(undefined)).toBe("N/A");
  });

  it("formats a valid date string", () => {
    const result = formatDate("2024-01-15T10:30:00Z");
    expect(result).toBeTruthy();
    expect(result).not.toBe("N/A");
  });
});

describe("formatCodec", () => {
  it("returns 'Unknown' for undefined", () => {
    expect(formatCodec(undefined)).toBe("Unknown");
  });

  it("extracts and uppercases codec from MIME type", () => {
    expect(formatCodec("video/h264")).toBe("H264");
    expect(formatCodec("audio/aac")).toBe("AAC");
  });

  it("returns as-is if no slash", () => {
    expect(formatCodec("h264")).toBe("h264");
  });
});

describe("formatFormat", () => {
  it("returns 'Unknown' for undefined", () => {
    expect(formatFormat(undefined)).toBe("Unknown");
  });

  it("extracts format name from URN", () => {
    expect(formatFormat("urn:x-nmos:format:video")).toBe("Video");
    expect(formatFormat("urn:x-nmos:format:audio")).toBe("Audio");
    expect(formatFormat("urn:x-nmos:format:data")).toBe("Data");
  });
});

describe("formatBitRate", () => {
  it("returns 'N/A' for undefined", () => {
    expect(formatBitRate(undefined)).toBe("N/A");
  });

  it("formats kbps", () => {
    expect(formatBitRate(500)).toBe("500 kbps");
  });

  it("formats Mbps", () => {
    expect(formatBitRate(5000)).toBe("5.0 Mbps");
  });
});

describe("formatResolution", () => {
  it("returns 'N/A' for missing values", () => {
    expect(formatResolution(undefined, undefined)).toBe("N/A");
    expect(formatResolution(1920, undefined)).toBe("N/A");
  });

  it("formats resolution", () => {
    expect(formatResolution(1920, 1080)).toBe("1920x1080");
  });
});

describe("formatFrameRate", () => {
  it("returns 'N/A' for undefined", () => {
    expect(formatFrameRate(undefined)).toBe("N/A");
  });

  it("formats integer frame rate", () => {
    expect(formatFrameRate({ numerator: 25 })).toBe("25 fps");
  });

  it("formats fractional frame rate", () => {
    expect(formatFrameRate({ numerator: 30000, denominator: 1001 })).toBe(
      "29.97 fps",
    );
  });
});
