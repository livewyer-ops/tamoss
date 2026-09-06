import { useQuery } from "@tanstack/react-query";
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
import { useApi } from "@/contexts/ApiContext";
import { usePageTitle } from "@/hooks/usePageTitle";
import styles from "./ServicePage.module.css";

export default function ServicePage() {
  usePageTitle("TAMS Service");
  const api = useApi();
  const service = useQuery({
    queryKey: ["api", "service"],
    queryFn: () => api.getService(),
  });
  const storage = useQuery({
    queryKey: ["api", "storage"],
    queryFn: () => api.getStorageBackends(),
  });
  const refresh = () => {
    service.refetch();
    storage.refetch();
  };

  return (
    <Page>
      <PageHeader
        title={
          <span className={styles.serviceTitle}>
            <img
              className={styles.tamsLockup}
              src="/brand/tams-lockup-color-light.svg"
              alt="TAMS"
            />{" "}
            <span>Service</span>
          </span>
        }
        actions={
          <Button type="button" onClick={refresh}>
            <RefreshCw size={14} aria-hidden="true" /> Refresh
          </Button>
        }
      />
      <div className={surfaceStyles.stack}>
        <Panel title="Service identity">
          {service.isLoading ? (
            <QueryMessage loading />
          ) : service.error ? (
            <QueryMessage
              error={service.error}
              onRetry={() => service.refetch()}
            />
          ) : service.data ? (
            <dl className={surfaceStyles.definitionList}>
              <dt>Name</dt>
              <dd>{service.data.name}</dd>
              <dt>Type</dt>
              <dd className={surfaceStyles.mono}>{service.data.type}</dd>
              <dt>API version</dt>
              <dd>{service.data.api_version}</dd>
              <dt>Service version</dt>
              <dd>{service.data.service_version}</dd>
              <dt>Description</dt>
              <dd>{service.data.description || "No description"}</dd>
            </dl>
          ) : null}
        </Panel>
        <Panel title="Storage backends">
          {storage.isLoading ? (
            <QueryMessage loading />
          ) : storage.error ? (
            <QueryMessage
              error={storage.error}
              onRetry={() => storage.refetch()}
            />
          ) : !storage.data?.length ? (
            <QueryMessage empty={{ title: "No storage backends advertised" }} />
          ) : (
            <div className={surfaceStyles.tableWrap}>
              <table className={surfaceStyles.table}>
                <thead>
                  <tr>
                    <th>Backend</th>
                    <th>Product</th>
                    <th>Provider</th>
                    <th>Region</th>
                    <th>Tags</th>
                    <th>Role</th>
                  </tr>
                </thead>
                <tbody>
                  {storage.data.map((backend) => (
                    <tr key={backend.id}>
                      <td>
                        <strong>{backend.label || backend.id}</strong>
                        <div
                          className={`${surfaceStyles.secondary} ${surfaceStyles.mono}`}
                        >
                          {backend.id}
                        </div>
                      </td>
                      <td>{backend.store_product}</td>
                      <td>{backend.provider || "-"}</td>
                      <td>{backend.region || "-"}</td>
                      <td>
                        {!backend.tags || Object.keys(backend.tags).length === 0
                          ? "-"
                          : Object.entries(backend.tags)
                              .map(
                                ([name, value]) =>
                                  `${name}: ${Array.isArray(value) ? value.join(", ") : value}`,
                              )
                              .join(" · ")}
                      </td>
                      <td>
                        {backend.default_storage ? (
                          <StatusBadge tone="success">Default</StatusBadge>
                        ) : (
                          <StatusBadge>Available</StatusBadge>
                        )}
                      </td>
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
