export default function LoadingSpinner({
  message = "Loading...",
}: {
  message?: string;
}) {
  return (
    <div
      className="tamoss-panel mx-auto flex max-w-md flex-col items-center justify-center gap-3 rounded-2xl px-6 py-12"
      role="status"
    >
      <div className="h-11 w-11 animate-spin rounded-full border-[3px] border-lw-ink-100 border-t-tams-500" />
      <span className="text-sm font-medium text-lw-ink-500">{message}</span>
    </div>
  );
}
