import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react-swc";
import path from "path";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
    dedupe: ["react", "react-dom"],
  },
  optimizeDeps: {
    include: ["@panvex/ui"],
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
      }
    }
  }
});
