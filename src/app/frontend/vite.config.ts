import path from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const apiTarget =
  process.env.VITE_API_TARGET ||
  process.env.TAMOSS_API_URL ||
  "http://localhost:8000";
const consoleTarget =
  process.env.VITE_CONTROL_API_TARGET ||
  process.env.TAMOSS_CONSOLE_UPSTREAM ||
  apiTarget;

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  build: {
    chunkSizeWarningLimit: 600,
    manifest: true,
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: apiTarget,
        changeOrigin: true,
        rewrite: (apiPath) => apiPath.replace(/^\/api/, ""),
      },
      "/ui-api": {
        target: consoleTarget,
        changeOrigin: true,
      },
    },
  },
});
