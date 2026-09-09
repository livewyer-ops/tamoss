import { Component, type ReactNode } from "react";
import { Page, PageHeader, Panel, QueryMessage } from "./Surface";

const chunkReloadKey = "tamoss:last-chunk-reload";
const chunkReloadWindowMs = 60_000;

export function isStaleAssetError(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error);
  return /ChunkLoadError|Loading chunk|dynamically imported module|Importing a module script failed/i.test(
    message,
  );
}

export function reloadStaleAssetsOnce({
  storage = window.sessionStorage,
  now = Date.now(),
  reload = () => window.location.reload(),
}: {
  storage?: Pick<Storage, "getItem" | "setItem">;
  now?: number;
  reload?: () => void;
} = {}): boolean {
  try {
    const lastReload = Number(storage.getItem(chunkReloadKey));
    if (now - lastReload <= chunkReloadWindowMs) return false;
    storage.setItem(chunkReloadKey, String(now));
    reload();
    return true;
  } catch {
    return false;
  }
}

export function installStaleAssetRecovery(
  recover: () => boolean = () => reloadStaleAssetsOnce(),
): () => void {
  const onPreloadError = (event: Event) => {
    event.preventDefault();
    recover();
  };
  window.addEventListener("vite:preloadError", onPreloadError);
  return () => window.removeEventListener("vite:preloadError", onPreloadError);
}

export class RouteLoadBoundary extends Component<
  { children: ReactNode },
  { error: Error | null }
> {
  state = { error: null };

  static getDerivedStateFromError(error: unknown) {
    return {
      error: error instanceof Error ? error : new Error(String(error)),
    };
  }

  componentDidCatch(error: Error) {
    if (isStaleAssetError(error)) reloadStaleAssetsOnce();
  }

  render() {
    if (!this.state.error) return this.props.children;
    const staleAssets = isStaleAssetError(this.state.error);
    return (
      <Page>
        <PageHeader
          title={staleAssets ? "Console update required" : "View unavailable"}
        />
        <Panel>
          <QueryMessage
            error={
              new Error(
                staleAssets
                  ? "This view could not load the current console assets."
                  : "This view stopped unexpectedly. Reload the console and try again.",
              )
            }
            onRetry={() => window.location.reload()}
          />
        </Panel>
      </Page>
    );
  }
}
