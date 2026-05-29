import { describe, expect, it } from "vitest";
import {
  decimalSecondsToNanoseconds,
  hmsToNanoseconds,
  sampleDurationNanoseconds,
  secondsToNanoseconds,
  timerangeFromNanoseconds,
  timestampFromNanoseconds,
} from "@/utils/tams-time";

describe("tams-time", () => {
  it("converts decimal seconds without floating point drift", () => {
    expect(secondsToNanoseconds(0.1 + 0.2)).toBe(300_000_000n);
    expect(decimalSecondsToNanoseconds("1.600000")).toBe(1_600_000_000n);
  });

  it("converts ffmpeg duration fields to nanoseconds", () => {
    expect(hmsToNanoseconds("00", "01", "02.500000")).toBe(62_500_000_000n);
  });

  it("formats BBC TAMS timestamps and half-open timeranges", () => {
    expect(timestampFromNanoseconds(-1_600_000_000n)).toBe("-1:600000000");
    expect(timerangeFromNanoseconds(0n, 1_000_000_000n)).toBe("[0:0_1:0)");
  });

  it("derives sample duration from rational frame rates", () => {
    expect(sampleDurationNanoseconds({ numerator: 25, denominator: 1 })).toBe(
      40_000_000n,
    );
    expect(
      sampleDurationNanoseconds({ numerator: 30_000, denominator: 1001 }),
    ).toBe(33_366_667n);
  });
});
