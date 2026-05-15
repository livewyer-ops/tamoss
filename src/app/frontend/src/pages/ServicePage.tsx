import { useEffect, useState } from "react";
import { useApi } from "@/contexts/ApiContext";
import { useApiQuery } from "@/hooks/useApiQuery";
import { usePageTitle } from "@/hooks/usePageTitle";
import LoadingSpinner from "@/components/LoadingSpinner";
import ErrorMessage from "@/components/ErrorMessage";
import Badge from "@/components/Badge";
import InlineEditField from "@/components/InlineEditField";
import StateStrip from "@/components/StateStrip";
import ApiReferencePanel from "@/components/ApiReferencePanel";
import RawPayload from "@/components/RawPayload";
import SectionHeading from "@/components/SectionHeading";

export default function ServicePage() {
  usePageTitle("Service");
  const api = useApi();
  const [snapshotAt, setSnapshotAt] = useState<string | null>(null);
  const health = useApiQuery(() => api.getHealth(), [api]);
  const service = useApiQuery(() => api.getService(), [api]);
  const backends = useApiQuery(() => api.getStorageBackends(), [api]);
  const rootPaths = useApiQuery(() => api.getRootPaths(), [api]);

  useEffect(() => {
    if (
      !health.loading &&
      !service.loading &&
      !backends.loading &&
      !rootPaths.loading
    ) {
      setSnapshotAt(new Date().toISOString());
    }
  }, [
    health.loading,
    service.loading,
    backends.loading,
    rootPaths.loading,
    health.data,
    service.data,
    backends.data,
    rootPaths.data,
  ]);

  if (
    health.loading ||
    service.loading ||
    backends.loading ||
    rootPaths.loading
  ) {
    return <LoadingSpinner message="Loading service details..." />;
  }

  if (health.error) {
    return (
      <div className="p-4 sm:p-6 lg:p-8">
        <ErrorMessage
          title="Health check failed"
          message={health.error}
          onRetry={health.refetch}
        />
      </div>
    );
  }

  if (service.error) {
    return (
      <div className="p-4 sm:p-6 lg:p-8">
        <ErrorMessage message={service.error} onRetry={service.refetch} />
      </div>
    );
  }

  if (backends.error) {
    return (
      <div className="p-4 sm:p-6 lg:p-8">
        <ErrorMessage message={backends.error} onRetry={backends.refetch} />
      </div>
    );
  }

  if (rootPaths.error) {
    return (
      <div className="p-4 sm:p-6 lg:p-8">
        <ErrorMessage message={rootPaths.error} onRetry={rootPaths.refetch} />
      </div>
    );
  }

  return (
    <div className="p-4 sm:p-6 lg:p-8">
      <div className="mb-6 flex items-center justify-between gap-4">
        <div>
          <h1 className="text-xl font-bold text-gray-900 sm:text-2xl">
            Service
          </h1>
          <p className="mt-1 text-sm text-gray-500">
            Operational details for this TAMOSS instance
          </p>
        </div>
        <button
          onClick={() => {
            health.refetch();
            service.refetch();
            backends.refetch();
            rootPaths.refetch();
          }}
          className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50"
        >
          Refresh
        </button>
      </div>

      <StateStrip
        title="Service State"
        refreshedAt={snapshotAt}
        items={[
          {
            label: "healthz",
            value:
              (health.data ?? "").trim().toLowerCase() === "ok"
                ? "ok"
                : "unknown",
            variant:
              (health.data ?? "").trim().toLowerCase() === "ok"
                ? "success"
                : "warning",
          },
          {
            label: "api",
            value: service.error ? "failing" : "reachable",
            variant: service.error ? "danger" : "success",
          },
          {
            label: "storage",
            value: backends.error
              ? "failing"
              : backends.data?.length
                ? "configured"
                : "missing",
            variant: backends.error
              ? "danger"
              : backends.data?.length
                ? "success"
                : "warning",
          },
          {
            label: "routes",
            value: String(rootPaths.data?.length ?? 0),
            variant: "info",
          },
        ]}
      />

      <SectionHeading
        eyebrow="Summary"
        title="Service Overview"
        description="Review service metadata, event integrations, available routes, storage backends, and raw API truth in one place."
      />

      {service.data && (
        <div className="mb-8 grid gap-6 lg:grid-cols-[2fr_1fr]">
          <section className="tamoss-panel rounded-2xl p-6">
            <SectionHeading title="State & Metadata" />
            <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <dt className="text-sm font-medium text-gray-500">Name</dt>
                <dd className="mt-1 text-sm text-gray-900">
                  <InlineEditField
                    value={service.data.name ?? ""}
                    placeholder="Set service name"
                    onSave={async (value) => {
                      await api.updateServiceInfo({
                        name: value || undefined,
                        description: service.data?.description,
                      });
                      service.refetch();
                    }}
                  />
                </dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500">Type</dt>
                <dd className="mt-1 text-sm text-gray-900">
                  {service.data.type ?? "N/A"}
                </dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500">
                  API version
                </dt>
                <dd className="mt-1 text-sm text-gray-900">
                  {service.data.api_version ?? "N/A"}
                </dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500">
                  Service version
                </dt>
                <dd className="mt-1 text-sm text-gray-900">
                  {service.data.service_version ?? "N/A"}
                </dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500">
                  Min object timeout
                </dt>
                <dd className="mt-1 font-mono text-sm text-gray-900">
                  {service.data.min_object_timeout ?? "N/A"}
                </dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500">
                  Min presigned URL timeout
                </dt>
                <dd className="mt-1 font-mono text-sm text-gray-900">
                  {service.data.min_presigned_url_timeout ?? "N/A"}
                </dd>
              </div>
            </dl>
            <div className="mt-4">
              <p className="mb-2 text-sm font-medium text-gray-500">
                Description
              </p>
              <div className="text-sm text-gray-600">
                <InlineEditField
                  value={service.data.description ?? ""}
                  placeholder="Set service description"
                  multiline
                  onSave={async (value) => {
                    await api.updateServiceInfo({
                      name: service.data?.name,
                      description: value || undefined,
                    });
                    service.refetch();
                  }}
                />
              </div>
            </div>
          </section>

          <section className="tamoss-panel rounded-2xl p-6">
            <SectionHeading title="Integrations" />
            <div className="flex flex-wrap gap-2">
              {service.data.event_stream_mechanisms?.length ? (
                service.data.event_stream_mechanisms.map((mechanism) => (
                  <a
                    key={mechanism.name ?? mechanism.docs ?? "event-mechanism"}
                    href={mechanism.docs ?? undefined}
                    target={mechanism.docs ? "_blank" : undefined}
                    rel={mechanism.docs ? "noreferrer" : undefined}
                    className="inline-flex"
                  >
                    <Badge variant="info">{mechanism.name ?? "Unknown"}</Badge>
                  </a>
                ))
              ) : (
                <p className="text-sm text-gray-500">
                  No event mechanisms advertised.
                </p>
              )}
            </div>
          </section>
        </div>
      )}

      <section className="mb-8 tamoss-panel rounded-2xl p-6">
        <SectionHeading title="Routes & Storage" />
        <div className="mb-6 flex flex-wrap gap-2">
          {rootPaths.data?.map((path) => (
            <Badge key={path} variant="default">
              /{path}
            </Badge>
          ))}
        </div>

        <SectionHeading title="Storage Backends" />
        {backends.data?.length ? (
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-gray-200">
              <thead>
                <tr>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">
                    ID
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">
                    Label
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">
                    Store
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">
                    Provider
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase text-gray-500">
                    Region
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {backends.data.map((backend, index) => (
                  <tr key={backend.id ?? index}>
                    <td className="px-4 py-3 font-mono text-xs text-gray-700">
                      {backend.id ?? "N/A"}
                    </td>
                    <td className="px-4 py-3 text-sm text-gray-900">
                      <div className="flex flex-wrap items-center gap-2">
                        <span>{backend.label ?? backend.id ?? "N/A"}</span>
                        {backend.default_storage ? (
                          <Badge variant="success">Default</Badge>
                        ) : null}
                      </div>
                    </td>
                    <td className="px-4 py-3 text-sm text-gray-500">
                      {backend.store_product ?? backend.store_type ?? "N/A"}
                    </td>
                    <td className="px-4 py-3 text-sm text-gray-500">
                      {backend.provider ?? "N/A"}
                    </td>
                    <td className="px-4 py-3 text-sm text-gray-500">
                      {backend.region ?? "N/A"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p className="text-sm text-gray-500">
            No storage backends configured.
          </p>
        )}
      </section>

      {service.data && (
        <RawPayload
          title="Raw Service Payload"
          description="Inspect the exact service response returned by the API."
          json={JSON.stringify(service.data, null, 2)}
        />
      )}

      <div className="mt-8 grid gap-6 lg:grid-cols-2">
        <SectionHeading
          eyebrow="Reference"
          title="API Reference"
          description="Copy exact endpoints and curl examples for operational use."
        />
        <ApiReferencePanel method="GET" path="/service" />
        <ApiReferencePanel
          title="Storage Backend API"
          method="GET"
          path="/service/storage-backends"
        />
      </div>
    </div>
  );
}
