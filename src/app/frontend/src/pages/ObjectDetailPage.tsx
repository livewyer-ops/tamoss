import { useQuery } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { Link, useParams, useSearchParams } from "react-router";
import {
  Button,
  Page,
  PageHeader,
  Panel,
  QueryMessage,
  surfaceStyles,
} from "@/components/Surface";
import { useApi } from "@/contexts/ApiContext";
import { usePageTitle } from "@/hooks/usePageTitle";
import { displayMediaLocation } from "@/utils/media-location";

export default function ObjectDetailPage() {
  usePageTitle("Media object");
  const { objectId = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const cursor = params.get("references") ?? undefined;
  const api = useApi();
  const object = useQuery({
    queryKey: ["api", "object", objectId, cursor],
    queryFn: () =>
      api.getObject(objectId, {
        limit: "50",
        page: cursor,
        presigned: false,
      }),
    enabled: Boolean(objectId),
  });
  const mediaObject = object.data?.data;
  const nextReferenceCursor = object.data?.nextKey;
  return (
    <Page>
      <PageHeader
        title="Media object"
        description={objectId}
        actions={
          <Link className={surfaceStyles.button} to="/objects">
            <ArrowLeft size={14} aria-hidden="true" /> Object lookup
          </Link>
        }
      />
      {object.isLoading ? (
        <Panel>
          <QueryMessage loading />
        </Panel>
      ) : object.error ? (
        <Panel>
          <QueryMessage error={object.error} onRetry={() => object.refetch()} />
        </Panel>
      ) : mediaObject ? (
        <div className={surfaceStyles.grid2}>
          <Panel title="References">
            <dl className={surfaceStyles.definitionList}>
              <dt>ID</dt>
              <dd className={surfaceStyles.mono}>{mediaObject.id}</dd>
              <dt>Timerange</dt>
              <dd className={surfaceStyles.mono}>
                {mediaObject.timerange || "-"}
              </dd>
              <dt>Initialisation Object</dt>
              <dd>
                {mediaObject.init_object ? (
                  <>
                    <Link
                      className={surfaceStyles.mono}
                      to={`/objects/${encodeURIComponent(mediaObject.init_object.id)}`}
                    >
                      {mediaObject.init_object.id}
                    </Link>
                    <div className={surfaceStyles.secondary}>
                      {mediaObject.init_object.get_urls?.length ?? 0} locations
                    </div>
                  </>
                ) : (
                  "-"
                )}
              </dd>
              <dt>First flow</dt>
              <dd>
                {mediaObject.first_referenced_by_flow ? (
                  <Link
                    className={surfaceStyles.mono}
                    to={`/flows/${mediaObject.first_referenced_by_flow}`}
                  >
                    {mediaObject.first_referenced_by_flow}
                  </Link>
                ) : (
                  "-"
                )}
              </dd>
              <dt>Referenced by</dt>
              <dd>
                {mediaObject.referenced_by_flows?.map((id) => (
                  <div key={id}>
                    <Link className={surfaceStyles.mono} to={`/flows/${id}`}>
                      {id}
                    </Link>
                  </div>
                )) || "-"}
              </dd>
            </dl>
            <footer className={surfaceStyles.pager}>
              <span>
                {mediaObject.referenced_by_flows?.length ?? 0} references on
                this page
              </span>
              <div className={surfaceStyles.toolbar}>
                <Button
                  type="button"
                  disabled={!cursor}
                  onClick={() =>
                    setParams((current) => {
                      const next = new URLSearchParams(current);
                      next.delete("references");
                      return next;
                    })
                  }
                >
                  First page
                </Button>
                <Button
                  type="button"
                  disabled={!nextReferenceCursor}
                  onClick={() =>
                    setParams((current) => {
                      const next = new URLSearchParams(current);
                      if (nextReferenceCursor)
                        next.set("references", nextReferenceCursor);
                      return next;
                    })
                  }
                >
                  Next
                </Button>
              </div>
            </footer>
          </Panel>
          <Panel title="Locations">
            {!mediaObject.get_urls?.length ? (
              <QueryMessage
                empty={{ title: "No readable locations returned" }}
              />
            ) : (
              <div className={surfaceStyles.tableWrap}>
                <table className={surfaceStyles.table}>
                  <thead>
                    <tr>
                      <th>Location</th>
                      <th>Storage</th>
                      <th>Access</th>
                    </tr>
                  </thead>
                  <tbody>
                    {mediaObject.get_urls.map((location) => (
                      <tr key={location.url}>
                        <td className={surfaceStyles.mono}>
                          {displayMediaLocation(location.url)}
                        </td>
                        <td>
                          {location.label || location.storage_id || "External"}
                        </td>
                        <td>{location.presigned ? "Presigned" : "Direct"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>
        </div>
      ) : null}
    </Page>
  );
}
