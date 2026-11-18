import { Play } from "lucide-react";
import { type FormEvent, lazy, Suspense, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import {
  Button,
  Page,
  PageHeader,
  Panel,
  QueryMessage,
  surfaceStyles,
} from "@/components/Surface";
import { usePageTitle } from "@/hooks/usePageTitle";

const MediaPreview = lazy(() => import("@/player/MediaPreview"));

export default function PlaybackPage() {
  usePageTitle("Preview");
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const selectedFlow = params.get("flow") ?? "";
  const [flowId, setFlowId] = useState(selectedFlow);
  function submit(event: FormEvent) {
    event.preventDefault();
    if (flowId.trim())
      navigate(`/playback?flow=${encodeURIComponent(flowId.trim())}`);
  }
  return (
    <Page>
      <PageHeader
        title="Preview"
        actions={
          <form className={surfaceStyles.toolbar} onSubmit={submit}>
            <label className="srOnly" htmlFor="preview-flow">
              Flow ID
            </label>
            <input
              id="preview-flow"
              className={`${surfaceStyles.input} ${surfaceStyles.mono}`}
              placeholder="Flow ID"
              value={flowId}
              onChange={(event) => setFlowId(event.target.value)}
            />
            <Button type="submit" variant="primary" disabled={!flowId.trim()}>
              <Play size={14} aria-hidden="true" /> Load
            </Button>
          </form>
        }
      />
      {selectedFlow ? (
        <Suspense
          fallback={
            <Panel>
              <QueryMessage loading />
            </Panel>
          }
        >
          <MediaPreview flowId={selectedFlow} />
        </Suspense>
      ) : (
        <Panel>
          <QueryMessage empty={{ title: "Select a flow to preview" }} />
        </Panel>
      )}
    </Page>
  );
}
