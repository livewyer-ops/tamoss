import { RefreshCw } from "lucide-react";
import {
  Button,
  Page,
  PageHeader,
  Panel,
  QueryMessage,
  StatusBadge,
  surfaceStyles,
} from "@/components/Surface";
import { useRuntime } from "@/control/runtime";
import { usePageTitle } from "@/hooks/usePageTitle";
import { formatRelativeTime } from "@/utils/format";

export default function IngestPage() {
  usePageTitle("Tamsin jobs");
  const runtime = useRuntime();
  const jobs =
    runtime.data?.jobs.filter(
      (job) =>
        job.component?.toLowerCase().includes("tamsin") ||
        job.name.toLowerCase().includes("tamsin"),
    ) ?? [];
  return (
    <Page>
      <PageHeader title="Tamsin jobs" />
      <div className={surfaceStyles.stack}>
        <Panel
          title="Observed Kubernetes Jobs"
          actions={
            <Button type="button" onClick={() => runtime.refetch()}>
              <RefreshCw size={14} aria-hidden="true" /> Refresh
            </Button>
          }
        >
          {runtime.isLoading ? (
            <QueryMessage loading />
          ) : runtime.error && !runtime.data ? (
            <QueryMessage
              error={runtime.error}
              onRetry={() => runtime.refetch()}
            />
          ) : jobs.length === 0 ? (
            <QueryMessage empty={{ title: "No Tamsin jobs observed" }} />
          ) : (
            <div className={surfaceStyles.tableWrap}>
              <table className={surfaceStyles.table}>
                <thead>
                  <tr>
                    <th>Job</th>
                    <th>Status</th>
                    <th>Active</th>
                    <th>Succeeded</th>
                    <th>Failed</th>
                    <th>Started</th>
                  </tr>
                </thead>
                <tbody>
                  {jobs.map((job) => (
                    <tr key={job.name}>
                      <td className={surfaceStyles.mono}>{job.name}</td>
                      <td>
                        <StatusBadge
                          tone={
                            job.status === "failed"
                              ? "error"
                              : job.status === "succeeded"
                                ? "success"
                                : "info"
                          }
                        >
                          {job.status}
                        </StatusBadge>
                      </td>
                      <td>{job.active}</td>
                      <td>{job.succeeded}</td>
                      <td>{job.failed}</td>
                      <td>{formatRelativeTime(job.startTime)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Panel>
      </div>
    </Page>
  );
}
