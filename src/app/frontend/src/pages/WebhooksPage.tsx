import { RefreshCw } from "lucide-react";
import { useCallback } from "react";
import { useSearchParams } from "react-router";
import { CatalogPager } from "@/components/CatalogToolbar";
import {
  Button,
  Page,
  PageHeader,
  Panel,
  QueryMessage,
  StatusBadge,
  surfaceStyles,
} from "@/components/Surface";
import { useApi } from "@/contexts/ApiContext";
import { useCursorPage } from "@/hooks/useCursorPage";
import { usePageTitle } from "@/hooks/usePageTitle";
import { displaySafeUrl } from "@/utils/media-location";

function tone(status?: string) {
  if (status === "started") return "success" as const;
  if (status === "error") return "error" as const;
  if (status === "disabled") return "warning" as const;
  return "neutral" as const;
}

export default function WebhooksPage() {
  usePageTitle("Webhooks");
  const api = useApi();
  const [params, setParams] = useSearchParams();
  const cursor = params.get("cursor") ?? undefined;
  const setCursor = useCallback(
    (value?: string) =>
      setParams((current) => {
        const next = new URLSearchParams(current);
        value ? next.set("cursor", value) : next.delete("cursor");
        return next;
      }),
    [setParams],
  );
  const load = useCallback(
    (page?: string) => api.getWebhooks({ limit: "50", page }),
    [api],
  );
  const webhooks = useCursorPage({ cursor, load, onCursorChange: setCursor });
  return (
    <Page>
      <PageHeader
        title="Webhooks"
        actions={
          <Button type="button" onClick={webhooks.refresh}>
            <RefreshCw size={14} aria-hidden="true" /> Refresh
          </Button>
        }
      />
      <Panel title="Registrations">
        {webhooks.loading ? (
          <QueryMessage loading />
        ) : webhooks.error ? (
          <QueryMessage error={webhooks.error} onRetry={webhooks.refresh} />
        ) : webhooks.data.length === 0 ? (
          <QueryMessage empty={{ title: "No webhooks registered" }} />
        ) : (
          <div className={surfaceStyles.tableWrap}>
            <table className={surfaceStyles.table}>
              <thead>
                <tr>
                  <th>Endpoint</th>
                  <th>Events</th>
                  <th>Filters</th>
                  <th>Status</th>
                  <th>Last error</th>
                </tr>
              </thead>
              <tbody>
                {webhooks.data.map((webhook) => (
                  <tr key={webhook.id}>
                    <td>
                      <strong>{displaySafeUrl(webhook.url)}</strong>
                      <div
                        className={`${surfaceStyles.secondary} ${surfaceStyles.mono}`}
                      >
                        {webhook.id}
                      </div>
                    </td>
                    <td>{webhook.events.join(", ")}</td>
                    <td>
                      {(webhook.flow_ids?.length ?? 0) +
                        (webhook.source_ids?.length ?? 0)}{" "}
                      resource filters
                    </td>
                    <td>
                      <StatusBadge tone={tone(webhook.status)}>
                        {webhook.status || "created"}
                      </StatusBadge>
                    </td>
                    <td>{webhook.error?.summary || "-"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <CatalogPager
          itemCount={webhooks.data.length}
          hasPrevious={webhooks.hasPrevious}
          hasNext={webhooks.hasNext}
          loading={webhooks.loading}
          onPrevious={webhooks.previous}
          onNext={webhooks.next}
          onRefresh={webhooks.refresh}
        />
      </Panel>
    </Page>
  );
}
