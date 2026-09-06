import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Radio } from "lucide-react";
import { Fragment } from "react";
import { Link, useParams } from "react-router";
import {
  Page,
  PageHeader,
  Panel,
  QueryMessage,
  StatusBadge,
  surfaceStyles,
} from "@/components/Surface";
import { useApi } from "@/contexts/ApiContext";
import { usePageTitle } from "@/hooks/usePageTitle";
import type { FlowEssenceParameters } from "@/types/tams";
import {
  formatBitRate,
  formatCodec,
  formatDate,
  formatFormat,
  formatFrameRate,
  formatResolution,
} from "@/utils/format";

function formatDuration(duration?: {
  numerator: number;
  denominator: number;
}): string {
  if (!duration) return "N/A";
  const seconds = duration.numerator / duration.denominator;
  return `${Number.isInteger(seconds) ? seconds : seconds.toFixed(3)} s`;
}

export default function ProfileDetailPage() {
  usePageTitle("Profile");
  const { profileId = "" } = useParams();
  const api = useApi();
  const profile = useQuery({
    queryKey: ["api", "profile", profileId],
    queryFn: () => api.getProfile(profileId),
    enabled: Boolean(profileId),
  });
  const metadata = profile.data?.flow_metadata;
  const essence = metadata?.essence_parameters as
    | FlowEssenceParameters
    | undefined;

  return (
    <Page>
      <PageHeader
        title={profile.data?.label || "Profile"}
        description={profileId}
        actions={
          <>
            <Link className={surfaceStyles.button} to="/profiles">
              <ArrowLeft size={14} aria-hidden="true" /> Profiles
            </Link>
            <Link
              className={surfaceStyles.button}
              to={`/flows?profile_id=${encodeURIComponent(profileId)}`}
            >
              <Radio size={14} aria-hidden="true" /> Matching flows
            </Link>
          </>
        }
      />
      {profile.isLoading ? (
        <Panel>
          <QueryMessage loading />
        </Panel>
      ) : profile.error ? (
        <Panel>
          <QueryMessage
            error={profile.error}
            onRetry={() => profile.refetch()}
          />
        </Panel>
      ) : profile.data && metadata ? (
        <div className={surfaceStyles.stack}>
          <div className={surfaceStyles.grid2}>
            <Panel title="Identity">
              <dl className={surfaceStyles.definitionList}>
                <dt>ID</dt>
                <dd className={surfaceStyles.mono}>{profile.data.id}</dd>
                <dt>Description</dt>
                <dd>{profile.data.description || "No description"}</dd>
                <dt>Created by</dt>
                <dd>{profile.data.created_by || "N/A"}</dd>
                <dt>Created</dt>
                <dd>{formatDate(profile.data.created)}</dd>
                <dt>Lifecycle</dt>
                <dd>
                  <StatusBadge tone="info">Immutable</StatusBadge>
                </dd>
              </dl>
            </Panel>
            <Panel title="Technical metadata">
              <dl className={surfaceStyles.definitionList}>
                <dt>Format</dt>
                <dd>
                  <StatusBadge tone="info">
                    {formatFormat(metadata.format)}
                  </StatusBadge>
                </dd>
                <dt>Codec</dt>
                <dd>{formatCodec(metadata.codec)}</dd>
                <dt>Container</dt>
                <dd>{metadata.container || "N/A"}</dd>
                <dt>Resolution</dt>
                <dd>
                  {formatResolution(
                    essence?.frame_width,
                    essence?.frame_height,
                  )}
                </dd>
                <dt>Frame rate</dt>
                <dd>{formatFrameRate(essence?.frame_rate)}</dd>
                <dt>Average bitrate</dt>
                <dd>{formatBitRate(metadata.avg_bit_rate)}</dd>
                <dt>Segment duration</dt>
                <dd>{formatDuration(metadata.segment_duration)}</dd>
                <dt>Initialisation segments</dt>
                <dd>{essence?.init_segments ? "Required" : "Not required"}</dd>
              </dl>
            </Panel>
          </div>
          <Panel title="Tags">
            {!Object.keys(profile.data.tags ?? {}).length ? (
              <QueryMessage empty={{ title: "No tags" }} />
            ) : (
              <dl className={surfaceStyles.definitionList}>
                {Object.entries(profile.data.tags ?? {}).map(
                  ([name, value]) => (
                    <Fragment key={name}>
                      <dt>{name}</dt>
                      <dd>{Array.isArray(value) ? value.join(", ") : value}</dd>
                    </Fragment>
                  ),
                )}
              </dl>
            )}
          </Panel>
        </div>
      ) : null}
    </Page>
  );
}
