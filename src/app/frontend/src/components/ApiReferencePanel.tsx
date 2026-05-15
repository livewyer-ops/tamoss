import CopyButton from "@/components/CopyButton";
import { buildApiReferenceUrl } from "@/components/apiReferenceUrl";

export default function ApiReferencePanel({
  title = "API Reference",
  method,
  path,
}: {
  title?: string;
  method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE" | "HEAD";
  path: string;
}) {
  const url = buildApiReferenceUrl(path);
  const curl = `curl -sS -H "Authorization: Bearer <token>" -X ${method} "${url}"`;

  return (
    <details className="group rounded-2xl border border-gray-800 bg-gray-950 p-6">
      <summary className="flex cursor-pointer list-none items-center justify-between gap-3">
        <div>
          <p className="text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-gray-400">
            Reference
          </p>
          <h2 className="mt-1 text-base font-semibold text-white">{title}</h2>
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
      <div className="mt-4 flex items-center justify-end gap-2">
        <CopyButton text={url} label="Copy URL" />
        <CopyButton text={curl} label="Copy curl" />
      </div>
      <div className="mt-3 space-y-3 text-xs text-gray-200">
        <div>
          <p className="mb-1 font-semibold uppercase tracking-wide text-gray-400">
            Endpoint
          </p>
          <code className="break-all text-gray-100">
            {method} {url}
          </code>
        </div>
        <div>
          <p className="mb-1 font-semibold uppercase tracking-wide text-gray-400">
            Curl
          </p>
          <pre className="overflow-x-auto whitespace-pre-wrap text-gray-200">
            {curl}
          </pre>
        </div>
      </div>
    </details>
  );
}
