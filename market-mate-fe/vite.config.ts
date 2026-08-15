import { defineConfig } from "vite";
import react from "@vitejs/plugin-react-swc";
import path from "path";
import { componentTagger } from "lovable-tagger";

const apiTarget = process.env.VITE_API_PROXY_TARGET || "http://localhost:8080";

// https://vitejs.dev/config/
export default defineConfig(({ mode }) => ({
  server: {
    host: "::",
    // Was 8080 — the same port the Go backend defaults to, so running both as
    // the README documents meant one of them failed to bind. 5173 is Vite's
    // default and is what the backend's CORS allow-list already expects.
    port: 5173,
    proxy: {
      // Proxying /api makes the frontend same-origin with the API in dev,
      // matching the production single-host deploy and letting
      // VITE_API_BASE_URL stay empty.
      "/api": { target: apiTarget, changeOrigin: true },
    },
  },
  preview: {
    port: 4173,
    proxy: {
      "/api": { target: apiTarget, changeOrigin: true },
    },
  },
  plugins: [react(), mode === "development" && componentTagger()].filter(Boolean),
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
}));
