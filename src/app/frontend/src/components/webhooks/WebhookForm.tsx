import { FormEvent, useState } from "react";
import ErrorMessage from "@/components/ErrorMessage";
import { StorageBackendMultiSelect } from "@/components/StorageBackendSelector";
import { parseCsv, supportedEvents } from "@/pages/webhooksModel";
import type {
  StorageBackend,
  WebhookEvent,
  WebhookDetail,
  WebhookWritePayload,
} from "@/types/tams";

export default function WebhookForm({
  initial,
  submitLabel,
  busy,
  onSubmit,
  onCancel,
  storageBackends,
}: {
  initial?: Partial<WebhookDetail>;
  submitLabel: string;
  busy: boolean;
  onSubmit: (payload: WebhookWritePayload) => Promise<void>;
  onCancel?: () => void;
  storageBackends?: StorageBackend[] | null;
}) {
  const [url, setUrl] = useState(initial?.url ?? "");
  const [apiKeyName, setApiKeyName] = useState(initial?.api_key_name ?? "");
  const [apiKeyValue, setApiKeyValue] = useState("");
  const [status, setStatus] = useState<WebhookWritePayload["status"]>(
    initial?.status === "disabled" ? "disabled" : "created",
  );
  const [events, setEvents] = useState<WebhookEvent[]>(
    initial?.events ?? ["flows/created"],
  );
  const [flowIds, setFlowIds] = useState((initial?.flow_ids ?? []).join(", "));
  const [sourceIds, setSourceIds] = useState(
    (initial?.source_ids ?? []).join(", "),
  );
  const [presigned, setPresigned] = useState(Boolean(initial?.presigned));
  const [verboseStorage, setVerboseStorage] = useState(
    Boolean(initial?.verbose_storage),
  );
  const [acceptStorageIds, setAcceptStorageIds] = useState<string[]>(
    initial?.accept_storage_ids ?? [],
  );
  const [submitError, setSubmitError] = useState<string | null>(null);
  const fieldPrefix = `webhook-form-${initial?.id ?? "new"}`;

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitError(null);
    try {
      const payload: WebhookWritePayload = {
        url,
        api_key_name: apiKeyName || undefined,
        api_key_value: apiKeyValue || undefined,
        events,
        flow_ids: parseCsv(flowIds),
        source_ids: parseCsv(sourceIds),
        accept_storage_ids: acceptStorageIds.length
          ? acceptStorageIds
          : undefined,
        presigned,
        verbose_storage: verboseStorage,
        status,
      };
      await onSubmit(payload);
    } catch (error) {
      setSubmitError(error instanceof Error ? error.message : "Unknown error");
    }
  }

  return (
    <form className="space-y-4" onSubmit={handleSubmit}>
      <div>
        <label
          htmlFor={`${fieldPrefix}-url`}
          className="block text-sm font-medium text-gray-700"
        >
          Webhook URL
        </label>
        <input
          id={`${fieldPrefix}-url`}
          type="url"
          value={url}
          onChange={(event) => setUrl(event.target.value)}
          required
          className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500"
          placeholder="https://example.com/webhook"
        />
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <div>
          <label
            htmlFor={`${fieldPrefix}-api-key-name`}
            className="block text-sm font-medium text-gray-700"
          >
            API key header
          </label>
          <input
            id={`${fieldPrefix}-api-key-name`}
            type="text"
            value={apiKeyName}
            onChange={(event) => setApiKeyName(event.target.value)}
            className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500"
            placeholder="X-Webhook-Key"
          />
        </div>
        <div>
          <label
            htmlFor={`${fieldPrefix}-api-key-value`}
            className="block text-sm font-medium text-gray-700"
          >
            API key value
          </label>
          <input
            id={`${fieldPrefix}-api-key-value`}
            type="password"
            value={apiKeyValue}
            onChange={(event) => setApiKeyValue(event.target.value)}
            className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500"
            placeholder={
              initial?.api_key_name
                ? "Leave blank to keep current value"
                : "Shared secret"
            }
          />
        </div>
      </div>

      <div>
        <label
          htmlFor={`${fieldPrefix}-status`}
          className="block text-sm font-medium text-gray-700"
        >
          Status
        </label>
        <select
          id={`${fieldPrefix}-status`}
          value={status}
          onChange={(event) =>
            setStatus(event.target.value as WebhookWritePayload["status"])
          }
          className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500"
        >
          <option value="created">Enabled</option>
          <option value="disabled">Disabled</option>
        </select>
      </div>

      <div>
        <p className="mb-2 text-sm font-medium text-gray-700">Events</p>
        <div className="grid gap-2 sm:grid-cols-2">
          {supportedEvents.map((eventName) => (
            <label
              key={eventName}
              className="flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-700"
            >
              <input
                type="checkbox"
                checked={events.includes(eventName)}
                onChange={(event) => {
                  if (event.target.checked) {
                    setEvents((previous) => [...previous, eventName]);
                    return;
                  }
                  setEvents((previous) =>
                    previous.filter((value) => value !== eventName),
                  );
                }}
                className="h-4 w-4 rounded border-gray-300 text-tams-600 focus:ring-tams-500"
              />
              <span>{eventName}</span>
            </label>
          ))}
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <div>
          <label className="block text-sm font-medium text-gray-700">
            Flow filters
          </label>
          <input
            type="text"
            value={flowIds}
            onChange={(event) => setFlowIds(event.target.value)}
            className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 font-mono text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500"
            placeholder="uuid-1, uuid-2"
          />
        </div>
        <div>
          <label className="block text-sm font-medium text-gray-700">
            Source filters
          </label>
          <input
            type="text"
            value={sourceIds}
            onChange={(event) => setSourceIds(event.target.value)}
            className="mt-1 block w-full rounded-lg border border-gray-300 px-3 py-2 font-mono text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500"
            placeholder="uuid-1, uuid-2"
          />
        </div>
      </div>

      <div className="flex flex-wrap gap-4">
        <label className="flex items-center gap-2 text-sm text-gray-700">
          <input
            type="checkbox"
            checked={presigned}
            onChange={(event) => setPresigned(event.target.checked)}
            className="h-4 w-4 rounded border-gray-300 text-tams-600 focus:ring-tams-500"
          />
          Request presigned URLs
        </label>
        <label className="flex items-center gap-2 text-sm text-gray-700">
          <input
            type="checkbox"
            checked={verboseStorage}
            onChange={(event) => setVerboseStorage(event.target.checked)}
            className="h-4 w-4 rounded border-gray-300 text-tams-600 focus:ring-tams-500"
          />
          Include verbose storage metadata
        </label>
      </div>

      <StorageBackendMultiSelect
        label="Accepted storage backends"
        values={acceptStorageIds}
        onChange={setAcceptStorageIds}
        backends={storageBackends}
        disabled={busy}
      />

      {submitError && (
        <ErrorMessage title="Webhook save failed" message={submitError} />
      )}

      <div className="flex flex-wrap gap-2">
        <button
          type="submit"
          disabled={
            busy ||
            !url ||
            events.length === 0 ||
            (Boolean(apiKeyName) && !apiKeyValue && !initial?.api_key_name)
          }
          className="rounded-lg bg-tams-600 px-4 py-2 text-sm font-medium text-white shadow-sm hover:bg-tams-700 disabled:opacity-50"
        >
          {busy ? "Saving..." : submitLabel}
        </button>
        {onCancel && (
          <button
            type="button"
            onClick={onCancel}
            className="rounded-lg border border-gray-300 bg-white px-4 py-2 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50"
          >
            Cancel
          </button>
        )}
      </div>
    </form>
  );
}
