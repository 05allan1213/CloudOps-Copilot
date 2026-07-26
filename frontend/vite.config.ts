import { fileURLToPath, URL } from "node:url";
import vue from "@vitejs/plugin-vue";
import { configDefaults, defineConfig } from "vitest/config";

const apiProxyTarget = process.env.VITE_API_PROXY_TARGET || "http://localhost:8080";

export default defineConfig({
  plugins: [vue()],
  test: {
    exclude: [...configDefaults.exclude, "tests/e2e/**"],
  },
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    host: "0.0.0.0",
    port: 5173,
    proxy: {
      "/api": {
        target: apiProxyTarget,
        // Keep the browser origin and Host aligned so the same-origin command
        // guard can validate proxied requests in local development.
        changeOrigin: false,
      },
      "/healthz": {
        target: apiProxyTarget,
        changeOrigin: true,
      },
      "/readyz": {
        target: apiProxyTarget,
        changeOrigin: true,
      },
      "/ws": {
        target: apiProxyTarget.replace(/^http/, "ws"),
        ws: true,
      },
    },
  },
});
