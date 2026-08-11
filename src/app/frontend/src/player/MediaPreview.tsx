import { useQuery } from "@tanstack/react-query";
import { Panel, QueryMessage, StatusBadge } from "@/components/Surface";
import { useApi } from "@/contexts/ApiContext";
import { formatCodec, formatFormat } from "@/utils/format";

export default function MediaPreview({ flowId }: { flowId: string }) {
  const api = useApi();
  const flow = useQuery({
    queryKey: ["api", "preview", flowId, "flow"],
    queryFn: () => api.getFlow(flowId, { include_timerange: true }),
  });
  if (flow.isLoading)
    return (
      <Panel>
        <QueryMessage loading />
      </Panel>
    );
  if (flow.error)
    return (
      <Panel>
        <QueryMessage error={flow.error} />
      </Panel>
    );
  return (
    <Panel
      title={flow.data?.label || flowId}
      actions={
        <>
          <StatusBadge tone="info">
            {formatFormat(flow.data?.format)}
          </StatusBadge>
          <StatusBadge>{formatCodec(flow.data?.codec)}</StatusBadge>
        </>
      }
    >
      <QueryMessage empty={{ title: "Preview unavailable" }} />
    </Panel>
  );
}
