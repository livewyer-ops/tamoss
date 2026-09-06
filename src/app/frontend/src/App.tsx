import { QueryClientProvider } from "@tanstack/react-query";
import { lazy, Suspense } from "react";
import { BrowserRouter, Navigate, Route, Routes } from "react-router";
import { apiQueryClient } from "@/api/query";
import Layout from "@/components/Layout";
import { RouteLoadBoundary } from "@/components/RouteLoadBoundary";
import { QueryMessage } from "@/components/Surface";
import { ApiProvider } from "@/contexts/ApiContext";

const DashboardPage = lazy(() => import("@/pages/DashboardPage"));
const SourcesPage = lazy(() => import("@/pages/SourcesPage"));
const SourceDetailPage = lazy(() => import("@/pages/SourceDetailPage"));
const FlowsPage = lazy(() => import("@/pages/FlowsPage"));
const FlowDetailPage = lazy(() => import("@/pages/FlowDetailPage"));
const ProfilesPage = lazy(() => import("@/pages/ProfilesPage"));
const ProfileDetailPage = lazy(() => import("@/pages/ProfileDetailPage"));
const ObjectsPage = lazy(() => import("@/pages/ObjectsPage"));
const ObjectDetailPage = lazy(() => import("@/pages/ObjectDetailPage"));
const PlaybackPage = lazy(() => import("@/pages/PlaybackPage"));
const IngestPage = lazy(() => import("@/pages/IngestPage"));
const IngestRunDetailPage = lazy(() => import("@/pages/IngestRunDetailPage"));
const DeletionsPage = lazy(() => import("@/pages/DeletionsPage"));
const WebhooksPage = lazy(() => import("@/pages/WebhooksPage"));
const SystemPage = lazy(() => import("@/pages/SystemPage"));
const ServicePage = lazy(() => import("@/pages/ServicePage"));

export default function App() {
  return (
    <QueryClientProvider client={apiQueryClient}>
      <BrowserRouter>
        <ApiProvider>
          <RouteLoadBoundary>
            <Suspense fallback={<QueryMessage loading />}>
              <Routes>
                <Route element={<Layout />}>
                  <Route index element={<DashboardPage />} />
                  <Route path="sources" element={<SourcesPage />} />
                  <Route
                    path="sources/:sourceId"
                    element={<SourceDetailPage />}
                  />
                  <Route path="flows" element={<FlowsPage />} />
                  <Route path="flows/:flowId" element={<FlowDetailPage />} />
                  <Route path="profiles" element={<ProfilesPage />} />
                  <Route
                    path="profiles/:profileId"
                    element={<ProfileDetailPage />}
                  />
                  <Route path="objects" element={<ObjectsPage />} />
                  <Route
                    path="objects/:objectId"
                    element={<ObjectDetailPage />}
                  />
                  <Route path="playback" element={<PlaybackPage />} />
                  <Route path="ingest" element={<IngestPage />} />
                  <Route
                    path="ingest/:runName"
                    element={<IngestRunDetailPage />}
                  />
                  <Route path="deletions" element={<DeletionsPage />} />
                  <Route path="webhooks" element={<WebhooksPage />} />
                  <Route path="system" element={<SystemPage />} />
                  <Route path="service" element={<ServicePage />} />
                  <Route path="*" element={<Navigate to="/" replace />} />
                </Route>
              </Routes>
            </Suspense>
          </RouteLoadBoundary>
        </ApiProvider>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
