import { fileURLToPath, URL } from "node:url";
import ui from "@nuxt/ui/vite";
import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vite";

const fixtureRoot = fileURLToPath(new URL(".", import.meta.url));
const frontendRoot = fileURLToPath(new URL("../../..", import.meta.url));

export default defineConfig({
  root: fixtureRoot,
  publicDir: false,
  plugins: [
    vue(),
    ui({
      colorMode: false,
      icon: {
        clientBundle: {
          scan: true,
          sizeLimitKb: 256,
        },
      },
    }),
  ],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("../../../src", import.meta.url)),
    },
  },
  server: {
    fs: { allow: [frontendRoot] },
  },
});
