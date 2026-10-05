import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Builds perf.html the way a shipped app is built — no React development
// checks, which cost more than anything they would be measuring against.
// `vite build --config vite.perf.config.ts`, then serve dist-perf/.
export default defineConfig({
  base: "./",
  plugins: [react()],
  build: {
    outDir: "dist-perf",
    emptyOutDir: true,
    rollupOptions: { input: "perf.html" },
  },
});
