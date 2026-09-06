export function formatTimerange(timerange?: string): string {
  if (!timerange || timerange === "_") return "All time";
  if (timerange === "()") return "Empty";
  return timerange;
}

export function formatDate(dateStr?: string): string {
  if (!dateStr) return "N/A";
  try {
    return new Date(dateStr).toLocaleString();
  } catch {
    return dateStr;
  }
}

export function formatRelativeTime(dateStr?: string): string {
  if (!dateStr) return "N/A";
  const value = Date.parse(dateStr);
  if (Number.isNaN(value)) return dateStr;

  const diffMs = value - Date.now();
  const absSeconds = Math.round(Math.abs(diffMs) / 1000);
  const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

  if (absSeconds < 60) {
    return formatter.format(Math.round(diffMs / 1000), "second");
  }
  if (absSeconds < 3600) {
    return formatter.format(Math.round(diffMs / 60000), "minute");
  }
  if (absSeconds < 86400) {
    return formatter.format(Math.round(diffMs / 3600000), "hour");
  }
  return formatter.format(Math.round(diffMs / 86400000), "day");
}

export function formatCodec(codec?: string): string {
  if (!codec) return "Unknown";
  const parts = codec.split("/");
  return parts.length > 1 ? parts[1].toUpperCase() : codec;
}

export function formatFormat(format?: string): string {
  if (!format) return "Unknown";
  const match = format.match(/format:(\w+)/);
  return match ? match[1].charAt(0).toUpperCase() + match[1].slice(1) : format;
}

export function formatBitRate(rate?: number): string {
  if (!rate) return "N/A";
  if (rate >= 1000) return `${(rate / 1000).toFixed(1)} Mbps`;
  return `${rate} kbps`;
}

export function formatResolution(width?: number, height?: number): string {
  if (!width || !height) return "N/A";
  return `${width}x${height}`;
}

export function formatFrameRate(frameRate?: {
  numerator: number;
  denominator?: number;
}): string {
  if (!frameRate) return "N/A";
  const denom = frameRate.denominator ?? 1;
  const fps = frameRate.numerator / denom;
  return `${Number.isInteger(fps) ? fps : fps.toFixed(2)} fps`;
}
