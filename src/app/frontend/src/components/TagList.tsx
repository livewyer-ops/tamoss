import Badge from "./Badge";

interface TagListProps {
  tags?: Record<string, unknown>;
}

/**
 * Resolve a tag value that may be a plain string, an array of strings,
 * or a Pydantic anyOf wrapper like `{ actual_instance: "val", any_of_schemas: [...] }`.
 */
function resolveTagValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (Array.isArray(value)) return value.map(String).join(", ");
  if (value && typeof value === "object") {
    const obj = value as Record<string, unknown>;
    if ("actual_instance" in obj) {
      return resolveTagValue(obj.actual_instance);
    }
    return JSON.stringify(value);
  }
  return String(value);
}

export default function TagList({ tags }: TagListProps) {
  if (!tags || Object.keys(tags).length === 0) {
    return <span className="text-sm text-lw-ink-400">No tags</span>;
  }

  return (
    <div className="flex flex-wrap gap-1.5">
      {Object.entries(tags).map(([key, value]) => {
        const displayValue = resolveTagValue(value);
        return (
          <Badge key={key} variant="default">
            <span className="font-semibold">{key}:</span>{" "}
            <span className="ml-1">{displayValue}</span>
          </Badge>
        );
      })}
    </div>
  );
}
