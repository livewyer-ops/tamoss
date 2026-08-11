const REDACTED_CONSOLE_MESSAGE =
  "Omakase media error details were redacted by TAMOSS.";
const MAX_INSPECTION_DEPTH = 8;
const MAX_OBJECT_KEYS = 128;

type ConsoleError = (...data: unknown[]) => void;

const sensitiveValues = new Map<string, number>();
let originalConsoleError: ConsoleError | undefined;
let installedConsoleError: ConsoleError | undefined;

/**
 * Omakase 1.1.1 logs hls.js ErrorData directly. Keep signed URLs out of that
 * pinned third-party logging path while a preview owns them in memory.
 */
export function installSensitiveConsoleErrorRedaction(
  values: readonly string[],
): () => void {
  const ownedValues = new Set(values.filter((value) => value.length > 0));
  if (ownedValues.size === 0) return () => undefined;

  for (const value of ownedValues) {
    sensitiveValues.set(value, (sensitiveValues.get(value) ?? 0) + 1);
  }
  if (!installedConsoleError) {
    originalConsoleError = console.error;
    installedConsoleError = (...data: unknown[]) => {
      const output = originalConsoleError;
      if (!output) return;
      const knownValues = [...sensitiveValues.keys()];
      if (
        data.some((value) =>
          containsSensitiveValue(value, knownValues, new Set(), 0),
        )
      ) {
        output.call(console, REDACTED_CONSOLE_MESSAGE);
        return;
      }
      output.apply(console, data);
    };
    console.error = installedConsoleError;
  }

  let disposed = false;
  return () => {
    if (disposed) return;
    disposed = true;
    for (const value of ownedValues) {
      const remaining = (sensitiveValues.get(value) ?? 1) - 1;
      if (remaining > 0) sensitiveValues.set(value, remaining);
      else sensitiveValues.delete(value);
    }
    if (sensitiveValues.size > 0) return;
    if (console.error === installedConsoleError && originalConsoleError) {
      console.error = originalConsoleError;
    }
    installedConsoleError = undefined;
    originalConsoleError = undefined;
  };
}

function containsSensitiveValue(
  value: unknown,
  knownValues: readonly string[],
  seen: Set<object>,
  depth: number,
): boolean {
  if (typeof value === "string") {
    return knownValues.some((known) => value.includes(known));
  }
  if (value instanceof URL) {
    return knownValues.some((known) => value.toString().includes(known));
  }
  if (
    value === null ||
    (typeof value !== "object" && typeof value !== "function") ||
    depth >= MAX_INSPECTION_DEPTH
  ) {
    return false;
  }

  const object = value as object;
  if (seen.has(object)) return false;
  seen.add(object);
  if (value instanceof Error) {
    if (
      containsSensitiveValue(value.message, knownValues, seen, depth + 1) ||
      containsSensitiveValue(value.stack, knownValues, seen, depth + 1) ||
      containsSensitiveValue(
        (value as Error & { cause?: unknown }).cause,
        knownValues,
        seen,
        depth + 1,
      )
    ) {
      return true;
    }
  }

  let keys: string[];
  try {
    keys = Object.keys(object).slice(0, MAX_OBJECT_KEYS);
  } catch {
    return false;
  }
  for (const key of keys) {
    try {
      if (
        containsSensitiveValue(
          (object as Record<string, unknown>)[key],
          knownValues,
          seen,
          depth + 1,
        )
      ) {
        return true;
      }
    } catch {
      // Ignore hostile getters; no value was handed to the real console.
    }
  }
  return false;
}
