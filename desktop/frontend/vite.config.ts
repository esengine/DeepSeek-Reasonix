import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// base: "./" so built asset URLs are relative. Wails serves the embedded dist from
// the app root over the wails:// scheme, where absolute "/assets/..." URLs 404.
export default defineConfig({
  plugins: [react()],
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    target: "es2021",
  },
  server: {
    // Bind IPv4 — unset host listens on ::1, and the Wails dev proxy's [::1]
    // dial fails on Windows hosts where IPv6 loopback is filtered.
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
  },
});
