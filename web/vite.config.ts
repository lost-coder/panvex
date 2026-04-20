import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react-swc";
import path from "path";
import { defineConfig } from "vite";

// P2-FE-06 / F4-4: suppress source maps for the embedded build. The embed
// is served as static assets from the Go binary in production, so shipping
// `.map` files would expose the original TS/TSX source to anyone hitting
// the panel. `npm run build:embed` passes `--mode embed`; we key off that
// mode here so the dev server (`vite dev`) is unaffected.
export default defineConfig(({ mode }) => ({
  plugins: [react(), tailwindcss()],
  // Embed build lives under a runtime-configurable root_path (/pan, /Fxzx…).
  // A fixed absolute `base` would force every chunk-preload into
  //   `link.href = "/assets/…"`  — the literal slash bypasses the panel
  // mount. With a relative base Vite emits `"./assets/…"` and the browser
  // resolves each link against the panel's `<base href>` injected by
  // serveUIIndex, so the URL lands under the configured root. The dev
  // server (`vite dev`) keeps the default "/" base.
  base: mode === "embed" ? "./" : "/",
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
    dedupe: ["react", "react-dom"],
  },
  build: {
    // Embed build: never ship source maps (F4-4).
    // Other modes fall back to Vite's default (off for `vite build`, on
    // for the `vite dev` server). Explicit `false` for embed guards against
    // future default changes and against `vite build --sourcemap` at the CLI.
    sourcemap: mode === "embed" ? false : undefined,
    // P3-FE-02: manual vendor chunks. After Phase 4 the UI-kit lives inside
    // src/ui/ instead of a separate package, so we split by direct-dep
    // heavyweight modules (recharts/motion/radix/tanstack/react) to keep
    // the initial route payload small and long-term cacheable.
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) return undefined;
          // Recharts pulls in d3-* — bundle the whole subtree together so
          // it only loads with chart-bearing routes.
          if (
            id.includes("/node_modules/recharts/") ||
            id.includes("/node_modules/d3-") ||
            id.includes("/node_modules/victory-vendor/") ||
            id.includes("/node_modules/internmap/") ||
            id.includes("/node_modules/decimal.js-light/")
          ) {
            return "vendor-recharts";
          }
          if (
            id.includes("/node_modules/framer-motion/") ||
            id.includes("/node_modules/motion/") ||
            id.includes("/node_modules/motion-dom/") ||
            id.includes("/node_modules/motion-utils/")
          ) {
            return "vendor-motion";
          }
          if (id.includes("/node_modules/@radix-ui/")) {
            return "vendor-radix";
          }
          if (
            id.includes("/node_modules/@tanstack/react-router") ||
            id.includes("/node_modules/@tanstack/react-query") ||
            id.includes("/node_modules/@tanstack/router-core") ||
            id.includes("/node_modules/@tanstack/query-core") ||
            id.includes("/node_modules/@tanstack/history")
          ) {
            return "vendor-tanstack";
          }
          if (
            id.includes("/node_modules/react/") ||
            id.includes("/node_modules/react-dom/") ||
            id.includes("/node_modules/scheduler/") ||
            id.includes("/node_modules/use-sync-external-store/")
          ) {
            return "vendor-react";
          }
          if (id.includes("/node_modules/lucide-react/")) {
            return "vendor-lucide";
          }
          if (id.includes("/node_modules/qrcode.react/")) {
            return "vendor-qrcode";
          }
          if (id.includes("/node_modules/zod/")) {
            return "vendor-zod";
          }
          return "vendor";
        },
      },
    },
    // Keep the warning sensitive: after splitting, each chunk should fit.
    chunkSizeWarningLimit: 600,
  },
  server: {
    host: "0.0.0.0",
    port: 5173,
    proxy: {
      "/api/events": {
        target: "http://127.0.0.1:8080",
        ws: true,
        on: {
          error: () => {},
          proxyReqWs: (_proxyReq, _req, socket) => {
            socket.on("error", () => {});
          },
        },
      },
      "/api": {
        target: "http://127.0.0.1:8080",
      },
    },
  },
}));
