import { RefreshCw } from "lucide-react";
import { useCallback } from "react";
import { Link, useSearchParams } from "react-router";
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
import { formatDate, formatTimerange } from "@/utils/format";

function tone(status: string) {
  if (status === "done") return "success" as const;
  if (status === "error") return "error" as const;
  if (status === "started") return "warning" as const;
  return "neutral" as const;
}

export default function DeletionsPage() {
  usePageTitle("Deletion requests");
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
    (page: string | undefined, signal: AbortSignal) =>
      api.getDeletionRequests(
        { limit: "50", page, sort_by: "created", reverse_order: true },
        { signal },
      ),
    [api],
  );
  const requests = useCursorPage({ cursor, load, onCursorChange: setCursor });
  return (
    <Page>
      <PageHeader
        title="Deletion requests"
        actions={
          <Button type="button" onClick={requests.refresh}>
            <RefreshCw size={14} aria-hidden="true" /> Refresh
          </Button>
        }
      />
      <Panel title="Requests">
        {requests.loading ? (
          <QueryMessage loading />
        ) : requests.error ? (
          <QueryMessage error={requests.error} onRetry={requests.refresh} />
        ) : requests.data.length === 0 ? (
          <QueryMessage empty={{ title: "No deletion requests" }} />
        ) : (
          <div className={surfaceStyles.tableWrap}>
            <table className={surfaceStyles.table}>
              <thead>
                <tr>
                  <th>Request</th>
                  <th>Flow</th>
                  <th>Timerange</th>
                  <th>Status</th>
                  <th>Updated</th>
                  <th>Error</th>
                </tr>
              </thead>
              <tbody>
                {requests.data.map((request) => (
                  <tr key={request.id}>
                    <td className={surfaceStyles.mono}>{request.id}</td>
                    <td>
                      <Link
                        className={surfaceStyles.mono}
                        to={`/flows/${request.flow_id}`}
                      >
                        {request.flow_id}
                      </Link>
                    </td>
                    <td className={surfaceStyles.mono}>
                      {formatTimerange(request.timerange_to_delete)}
                    </td>
                    <td>
                      <StatusBadge tone={tone(request.status)}>
                        {request.status}
                      </StatusBadge>
                    </td>
                    <td>{formatDate(request.updated ?? request.created)}</td>
                    <td>{request.error?.summary || "-"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <CatalogPager
          itemCount={requests.data.length}
          hasPrevious={requests.hasPrevious}
          hasNext={requests.hasNext}
          loading={requests.loading}
          onPrevious={requests.previous}
          onNext={requests.next}
          onRefresh={requests.refresh}
        />
      </Panel>
    </Page>
  );
}
