import { fileURLToPath, URL } from "node:url";

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    port: 5173,
    // The backend deliberately serves no CORS headers: the dev server proxies
    // /api to moxyd so the browser only ever talks to one origin.
    proxy: {
      "/api": {
        target: process.env.MOXY_API ?? "http://127.0.0.1:8080",
        changeOrigin: true,
      },
    },
  },
});
