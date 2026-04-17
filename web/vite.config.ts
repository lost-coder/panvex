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
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
    dedupe: ["react", "react-dom"],
  },
  optimizeDeps: {
    include: ["@lost-coder/panvex-ui"],
  },
  build: {
    // Embed build: never ship source maps (F4-4).
    // Other modes fall back to Vite's default (off for `vite build`, on
    // for the `vite dev` server). Explicit `false` for embed guards against
    // future default changes and against `vite build --sourcemap` at the CLI.
    sourcemap: mode === "embed" ? false : undefined,
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
