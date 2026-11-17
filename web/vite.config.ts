import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// `npm run build` writes straight into the Go server's embed directory, so
// `go build` ships the UI inside the single folio binary.
//
// `npm run dev` serves the UI with hot reload on :5173 and proxies the API
// and fonts to a running `folio serve --no-open` on :7350.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/server/dist",
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    port: 5173,
    // changeOrigin must stay false: the desk refuses POSTs whose Origin
    // doesn't match Host (CSRF guard), and Vite's string shorthand would
    // rewrite Host to 127.0.0.1:7350.
    proxy: {
      "/api": { target: "http://127.0.0.1:7350", changeOrigin: false },
      "/fonts": { target: "http://127.0.0.1:7350", changeOrigin: false },
    },
  },
});
