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
