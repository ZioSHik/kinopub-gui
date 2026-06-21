import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Relative base so the embedded build works when served from the Go binary.
export default defineConfig({
  base: "./",
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    chunkSizeWarningLimit: 1200,
  },
  server: {
    port: 5173,
    proxy: {
      // During `npm run dev`, proxy API + SSE to the Go backend.
      "/api": {
        target: "http://127.0.0.1:8765",
        changeOrigin: true,
      },
    },
  },
});
