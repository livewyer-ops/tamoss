interface EmptyStateProps {
  title: string;
  description?: string;
  icon?: string;
}

export default function EmptyState({
  title,
  description,
  icon = "M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4",
}: EmptyStateProps) {
  return (
    <div className="tamoss-panel rounded-2xl px-6 py-14 text-center">
      <svg
        className="mx-auto mb-5 h-14 w-14 text-tams-500"
        fill="none"
        viewBox="0 0 24 24"
        strokeWidth={1}
        stroke="currentColor"
        aria-hidden="true"
      >
        <path strokeLinecap="round" strokeLinejoin="round" d={icon} />
      </svg>
      <h3 className="text-base font-semibold text-lw-ink-900">{title}</h3>
      {description && (
        <p className="mx-auto mt-2 max-w-lg text-sm leading-6 text-lw-ink-500">
          {description}
        </p>
      )}
    </div>
  );
}
