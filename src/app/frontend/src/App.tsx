import { BrowserRouter, Routes, Route } from "react-router";
import { QueryClientProvider } from "@tanstack/react-query";
import { apiQueryClient } from "@/api/query";
import { ApiProvider } from "@/contexts/ApiContext";
import { ToastProvider } from "@/components/Toaster";
import Layout from "@/components/Layout";
import DashboardPage from "@/pages/DashboardPage";
import ServicePage from "@/pages/ServicePage";
import SourcesPage from "@/pages/SourcesPage";
import SourceDetailPage from "@/pages/SourceDetailPage";
import FlowsPage from "@/pages/FlowsPage";
import FlowDetailPage from "@/pages/FlowDetailPage";
import ObjectsPage from "@/pages/ObjectsPage";
import ObjectDetailPage from "@/pages/ObjectDetailPage";
import PlaybackPage from "@/pages/PlaybackPage";
import IngestPage from "@/pages/IngestPage";
import DeletionsPage from "@/pages/DeletionsPage";
import WebhooksPage from "@/pages/WebhooksPage";

export default function App() {
  return (
    <QueryClientProvider client={apiQueryClient}>
      <BrowserRouter>
        <ApiProvider>
          <ToastProvider>
            <Routes>
              <Route element={<Layout />}>
                <Route index element={<DashboardPage />} />
                <Route path="service" element={<ServicePage />} />
                <Route path="sources" element={<SourcesPage />} />
                <Route
                  path="sources/:sourceId"
                  element={<SourceDetailPage />}
                />
                <Route path="flows" element={<FlowsPage />} />
                <Route path="flows/:flowId" element={<FlowDetailPage />} />
                <Route path="playback" element={<PlaybackPage />} />
                <Route path="objects" element={<ObjectsPage />} />
                <Route
                  path="objects/:objectId"
                  element={<ObjectDetailPage />}
                />
                <Route path="webhooks" element={<WebhooksPage />} />
                <Route path="ingest" element={<IngestPage />} />
                <Route path="deletions" element={<DeletionsPage />} />
              </Route>
            </Routes>
          </ToastProvider>
        </ApiProvider>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
