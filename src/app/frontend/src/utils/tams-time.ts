const NANOS_PER_SECOND = 1_000_000_000n;

export interface Rational {
  numerator: number;
  denominator?: number;
}

export function secondsToNanoseconds(seconds: number): bigint {
  if (!Number.isFinite(seconds) || seconds < 0) {
    throw new Error("seconds must be a non-negative finite number");
  }
  return decimalSecondsToNanoseconds(seconds.toFixed(9));
}

export function decimalSecondsToNanoseconds(value: string): bigint {
  const trimmed = value.trim();
  if (!/^\d+(?:\.\d+)?$/.test(trimmed)) {
    throw new Error("seconds must be a non-negative decimal string");
  }

  const [whole, fraction = ""] = trimmed.split(".");
  const paddedFraction = `${fraction}000000000`.slice(0, 9);
  return BigInt(whole) * NANOS_PER_SECOND + BigInt(paddedFraction);
}

export function hmsToNanoseconds(
  hours: string,
  minutes: string,
  seconds: string,
): bigint {
  return (
    BigInt(hours) * 3_600n * NANOS_PER_SECOND +
    BigInt(minutes) * 60n * NANOS_PER_SECOND +
    decimalSecondsToNanoseconds(seconds)
  );
}

export function sampleDurationNanoseconds(rate: Rational): bigint | undefined {
  const denominator = rate.denominator ?? 1;
  if (
    !Number.isInteger(rate.numerator) ||
    !Number.isInteger(denominator) ||
    rate.numerator <= 0 ||
    denominator <= 0
  ) {
    return undefined;
  }

  const numerator = BigInt(rate.numerator);
  return (BigInt(denominator) * NANOS_PER_SECOND + numerator / 2n) / numerator;
}

export function timestampFromNanoseconds(nanoseconds: bigint): string {
  const sign = nanoseconds < 0n ? "-" : "";
  const absolute = nanoseconds < 0n ? -nanoseconds : nanoseconds;
  const whole = absolute / NANOS_PER_SECOND;
  const fraction = absolute % NANOS_PER_SECOND;
  return `${sign}${whole}:${fraction}`;
}

export function timerangeFromNanoseconds(
  startNanoseconds: bigint,
  endNanoseconds: bigint,
): string {
  if (endNanoseconds < startNanoseconds) {
    throw new Error("timerange end must not be before start");
  }
  return `[${timestampFromNanoseconds(startNanoseconds)}_${timestampFromNanoseconds(
    endNanoseconds,
  )})`;
}

export interface TamsTimerangeBounds {
  start?: bigint;
  startInclusive: boolean;
  end: bigint;
  endInclusive: boolean;
  /** True for a point timerange: one instant included by both bounds. */
  instantaneous: boolean;
}

export interface HalfOpenTimerange {
  startNanoseconds: bigint;
  endNanoseconds: bigint;
  timerange: string;
}

export function nanosecondsFromTimestamp(
  timestamp: string,
): bigint | undefined {
  const match = /^(-?)(\d+):(\d{1,9})$/u.exec(timestamp);
  if (!match) return undefined;
  const nanoseconds = BigInt(match[3]);
  if (nanoseconds >= NANOS_PER_SECOND) return undefined;
  // The sign applies to the combined value, not to the seconds alone.
  const absolute = BigInt(match[2]) * NANOS_PER_SECOND + nanoseconds;
  return match[1] === "-" ? -absolute : absolute;
}

/**
 * Parses a TAMS timerange that has a bounded end.
 *
 * Accepts the point form `[t]` and the range forms `[a_b)`, `[a_b]`, `(a_b)`,
 * `(a_b]` and `_b)`. Bound inclusivity is reported rather than applied so that
 * callers can preserve the original TAMS form.
 */
export function boundsFromTimerange(
  timerange: string,
): TamsTimerangeBounds | undefined {
  const instant = /^\[(-?\d+:\d{1,9})\]$/u.exec(timerange);
  if (instant) {
    const timestamp = nanosecondsFromTimestamp(instant[1]);
    return timestamp === undefined
      ? undefined
      : {
          start: timestamp,
          startInclusive: true,
          end: timestamp,
          endInclusive: true,
          instantaneous: true,
        };
  }

  const range = /^(?:(\[|\()(-?\d+:\d{1,9})?)?_(-?\d+:\d{1,9})(\)|\])$/u.exec(
    timerange,
  );
  if (!range || (range[1] && !range[2])) return undefined;
  const start = range[2] ? nanosecondsFromTimestamp(range[2]) : undefined;
  const end = nanosecondsFromTimestamp(range[3]);
  if (end === undefined || (range[2] && start === undefined)) return undefined;
  if (start !== undefined && start > end) return undefined;
  const startInclusive = range[1] !== "(";
  const endInclusive = range[4] === "]";
  return {
    ...(start === undefined ? {} : { start }),
    startInclusive,
    end,
    endInclusive,
    instantaneous:
      start !== undefined && start === end && startInclusive && endInclusive,
  };
}

/**
 * Converts a bounded TAMS timerange to its exact half-open equivalent.
 *
 * TAMS timestamps are integer nanoseconds, so an inclusive end includes the
 * end instant and the half-open equivalent runs one nanosecond further. An
 * exclusive start excludes the start instant, so the half-open equivalent
 * begins one nanosecond later. A point timerange has zero playable duration
 * and an unbounded start has no half-open equivalent; both return undefined.
 */
export function halfOpenTimerange(
  timerange: string,
): HalfOpenTimerange | undefined {
  const bounds = boundsFromTimerange(timerange);
  if (!bounds || bounds.start === undefined || bounds.instantaneous) {
    return undefined;
  }
  const startNanoseconds = bounds.startInclusive
    ? bounds.start
    : bounds.start + 1n;
  const endNanoseconds = bounds.endInclusive ? bounds.end + 1n : bounds.end;
  if (endNanoseconds <= startNanoseconds) return undefined;
  return {
    startNanoseconds,
    endNanoseconds,
    timerange: timerangeFromNanoseconds(startNanoseconds, endNanoseconds),
  };
}
