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
  build: {
    // React and its DOM renderer are ~190 KB of the 287 KB bundle and change
    // when React does, which is to say almost never. moxyd serves /assets/
    // with immutable caching, so keeping them in a chunk of their own means a
    // release re-downloads the application and not the framework under it.
    rollupOptions: {
      output: {
        // A function rather than the `{ name: [modules] }` object: Vite 8
        // bundles with rolldown, which only accepts the callback form.
        manualChunks(id) {
          return /node_modules[/\\](react|react-dom|scheduler)[/\\]/.test(id)
            ? "react"
            : undefined;
        },
      },
    },
    // NO SOURCE MAPS. "hidden" would emit 1.4 MB of them into dist/, which
    // the image copies whole and moxyd would serve: dead weight for a
    // debugging aid nothing here consumes — there is no error tracker to
    // upload them to, and the source is public anyway. Turn it on the day
    // something reads them.
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
