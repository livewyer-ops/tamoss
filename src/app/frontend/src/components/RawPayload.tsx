import CopyButton from "@/components/CopyButton";

interface RawPayloadProps {
  title?: string;
  description?: string;
  json: string;
  defaultOpen?: boolean;
  className?: string;
}

export default function RawPayload({
  title = "Raw API payload",
  description,
  json,
  defaultOpen = false,
  className = "",
}: RawPayloadProps) {
  return (
    <details
      className={`group rounded-2xl border border-gray-800 bg-gray-950 p-4 sm:p-6 ${className}`.trim()}
      {...(defaultOpen ? { open: true } : {})}
    >
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3">
        <div>
          <p className="text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-gray-400">
            Raw
          </p>
          <p className="mt-1 text-base font-semibold text-white">{title}</p>
          {description && (
            <p className="mt-1.5 max-w-3xl text-xs leading-5 text-gray-400">
              {description}
            </p>
          )}
        </div>
        <span
          className="select-none rounded-md border border-gray-700 px-2 py-1 text-[0.65rem] font-medium uppercase tracking-wider text-gray-300 group-open:hidden"
          aria-hidden="true"
        >
          Expand
        </span>
        <span
          className="hidden select-none rounded-md border border-gray-700 px-2 py-1 text-[0.65rem] font-medium uppercase tracking-wider text-gray-300 group-open:inline-block"
          aria-hidden="true"
        >
          Collapse
        </span>
      </summary>
      <div className="mt-4 flex items-center justify-end">
        <CopyButton text={json} label="Copy JSON" />
      </div>
      <pre className="mt-3 overflow-x-auto text-xs text-gray-200">{json}</pre>
    </details>
  );
}
