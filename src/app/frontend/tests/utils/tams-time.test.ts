import { describe, expect, it } from "vitest";
import {
  boundsFromTimerange,
  halfOpenTimerange,
  nanosecondsFromTimestamp,
  timerangeFromNanoseconds,
  timestampFromNanoseconds,
} from "@/utils/tams-time";

describe("tams-time", () => {
  it("formats BBC TAMS timestamps and half-open timeranges", () => {
    expect(timestampFromNanoseconds(-1_600_000_000n)).toBe("-1:600000000");
    expect(timerangeFromNanoseconds(0n, 1_000_000_000n)).toBe("[0:0_1:0)");
  });

  it("applies the sign to the whole timestamp, not to the seconds alone", () => {
    expect(nanosecondsFromTimestamp("-1:500000000")).toBe(-1_500_000_000n);
    expect(nanosecondsFromTimestamp("9007199254740993:123456789")).toBe(
      9_007_199_254_740_993_123_456_789n,
    );
    expect(nanosecondsFromTimestamp("1:1000000000")).toBeUndefined();
    expect(nanosecondsFromTimestamp("1")).toBeUndefined();
  });

  it("reports the inclusivity of each timerange bound", () => {
    expect(boundsFromTimerange("[10:0_20:0)")).toEqual({
      start: 10_000_000_000n,
      startInclusive: true,
      end: 20_000_000_000n,
      endInclusive: false,
      instantaneous: false,
    });
    expect(boundsFromTimerange("(10:0_20:0]")).toEqual({
      start: 10_000_000_000n,
      startInclusive: false,
      end: 20_000_000_000n,
      endInclusive: true,
      instantaneous: false,
    });
    expect(boundsFromTimerange("_20:0)")).toEqual({
      startInclusive: true,
      end: 20_000_000_000n,
      endInclusive: false,
      instantaneous: false,
    });
    expect(boundsFromTimerange("[20:0_10:0)")).toBeUndefined();
    expect(boundsFromTimerange("[10:0_")).toBeUndefined();
  });

  it("treats a single included instant as a point timerange", () => {
    expect(boundsFromTimerange("[10:0]")).toMatchObject({
      start: 10_000_000_000n,
      end: 10_000_000_000n,
      instantaneous: true,
    });
    expect(boundsFromTimerange("[10:0_10:0]")).toMatchObject({
      instantaneous: true,
    });
    expect(boundsFromTimerange("[10:0_10:0)")).toMatchObject({
      instantaneous: false,
    });
  });

  it("converts an inclusive end to exactly one more nanosecond", () => {
    expect(halfOpenTimerange("[10:0_20:0]")).toEqual({
      startNanoseconds: 10_000_000_000n,
      endNanoseconds: 20_000_000_001n,
      timerange: "[10:0_20:1)",
    });
    expect(halfOpenTimerange("(10:0_20:0]")).toEqual({
      startNanoseconds: 10_000_000_001n,
      endNanoseconds: 20_000_000_001n,
      timerange: "[10:1_20:1)",
    });
    expect(halfOpenTimerange("(10:0_20:0)")).toEqual({
      startNanoseconds: 10_000_000_001n,
      endNanoseconds: 20_000_000_000n,
      timerange: "[10:1_20:0)",
    });
  });

  it("keeps a half-open timerange and its nanosecond precision unchanged", () => {
    expect(
      halfOpenTimerange(
        "[9007199254740993:123456789_9007199254741593:987654321)",
      ),
    ).toEqual({
      startNanoseconds: 9_007_199_254_740_993_123_456_789n,
      endNanoseconds: 9_007_199_254_741_593_987_654_321n,
      timerange: "[9007199254740993:123456789_9007199254741593:987654321)",
    });
  });

  it("reports no half-open equivalent when nothing is playable", () => {
    expect(halfOpenTimerange("[10:0]")).toBeUndefined();
    expect(halfOpenTimerange("[10:0_10:0]")).toBeUndefined();
    expect(halfOpenTimerange("[10:0_10:0)")).toBeUndefined();
    expect(halfOpenTimerange("(10:0_10:1)")).toBeUndefined();
    expect(halfOpenTimerange("_20:0)")).toBeUndefined();
    expect(halfOpenTimerange("not-a-timerange")).toBeUndefined();
  });
});
