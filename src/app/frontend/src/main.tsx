import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { installStaleAssetRecovery } from "./components/RouteLoadBoundary";
import "./index.css";

installStaleAssetRecovery();

const root = document.getElementById("root");
if (!root) throw new Error("TAMOSS UI root element is missing");

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
