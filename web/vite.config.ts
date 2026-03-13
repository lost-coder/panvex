import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react-swc";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5173,
    proxy: {
      "/auth": "http://127.0.0.1:8080",
      "/fleet": "http://127.0.0.1:8080",
      "/agents": "http://127.0.0.1:8080",
      "/instances": "http://127.0.0.1:8080",
      "/jobs": "http://127.0.0.1:8080",
      "/audit": "http://127.0.0.1:8080",
      "/metrics": "http://127.0.0.1:8080",
      "/events": {
        target: "ws://127.0.0.1:8080",
        ws: true
      }
    }
  }
});
