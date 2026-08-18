import { defineConfig } from "vite";
import react from "@vitejs/plugin-react-swc";
import path from "path";
import { componentTagger } from "lovable-tagger";

// Ports come from the environment so `make demo API_PORT=8081` moves both
// halves together. Hard-coding them is how the sibling repo ended up with a
// dev server proxying to a port its backend had stopped listening on.
const apiPort = process.env.API_PORT || "8080";
const webPort = Number(process.env.WEB_PORT || 5173);
const apiTarget = process.env.VITE_API_PROXY_TARGET || `http://localhost:${apiPort}`;

const proxy = { target: apiTarget, changeOrigin: true };

// https://vitejs.dev/config/
export default defineConfig(({ mode }) => ({
  server: {
    // `true` rather than "::". Binding the IPv6 wildcard explicitly fails with
    // EAFNOSUPPORT on any host without IPv6 — containers, some CI runners,
    // locked-down laptops — and the dev server simply refuses to start. `true`
    // asks Vite for all interfaces and it picks something that works.
    host: process.env.VITE_HOST || true,
    // Was 8080 — the same port the Go backend defaults to, so running both as
    // the README documents meant one of them failed to bind. 5173 is Vite's
    // default and is what the backend's CORS allow-list already expects.
    port: webPort,
    // Fail rather than sliding to the next free port: a dev server on an
    // unexpected port is the kind of thing you notice twenty minutes later.
    strictPort: true,
    proxy: {
      // Proxying these makes the frontend same-origin with the API in dev,
      // matching the production single-host deploy and letting
      // VITE_API_BASE_URL stay empty. /graphql and /graphiql are served by the
      // same backend and need the same treatment, or the SPA's GraphQL client
      // 404s against the Vite dev server instead of reaching the API.
      "/api": proxy,
      "/graphql": proxy,
      "/graphiql": proxy,
    },
  },
  preview: {
    port: 4173,
    proxy: {
      "/api": proxy,
      "/graphql": proxy,
      "/graphiql": proxy,
    },
  },
  plugins: [react(), mode === "development" && componentTagger()].filter(Boolean),
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
}));
