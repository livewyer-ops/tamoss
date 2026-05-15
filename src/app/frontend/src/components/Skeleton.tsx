interface SkeletonProps {
  className?: string;
  rows?: number;
}

export default function Skeleton({
  className = "h-4 w-full",
  rows = 1,
}: SkeletonProps) {
  if (rows <= 1) {
    return (
      <div
        className={`animate-pulse rounded bg-lw-ink-100 ${className}`}
        aria-hidden="true"
      />
    );
  }
  return (
    <div className="space-y-2" aria-hidden="true">
      {Array.from({ length: rows }, (_, i) => (
        <div
          key={i}
          className={`animate-pulse rounded bg-lw-ink-100 ${className}`}
        />
      ))}
    </div>
  );
}
