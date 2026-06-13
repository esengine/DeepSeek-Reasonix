import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import { execSync } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const devPort = Number(process.env.REASONIX_DESKTOP_VITE_PORT || "5173");
const configDir = dirname(fileURLToPath(import.meta.url));

// Stamps the build commit into the bundle so a minified crash stack can be mapped
// back to the sourcemap of the exact build. Falls back to "dev" off a git checkout.
function buildCommit(): string {
  if (process.env.REASONIX_COMMIT) return process.env.REASONIX_COMMIT;
  try {
    return execSync("git rev-parse --short HEAD", { cwd: configDir }).toString().trim();
  } catch {
    return "dev";
  }
}

// On macOS ≤ 12 (Safari 15 WebKit) a crossorigin module/stylesheet fetched over the
// wails:// scheme is CORS-blocked (no Access-Control-Allow-Origin from the handler),
// so the bundle never loads and the window paints blank; newer WebKit tolerates it.
function stripCrossorigin(): Plugin {
  return {
    name: "strip-crossorigin",
    enforce: "post",
    transformIndexHtml: (html) => html.replace(/\s+crossorigin(?==["']|[\s/>])/g, ""),
  };
}

// Vite must empty dist before production builds so stale hashed assets disappear.
// Recreate the tracked placeholder afterwards so git status stays clean and
// Go's //go:embed all:frontend/dist still works on a fresh checkout.
function keepDistPlaceholder(): Plugin {
  return {
    name: "keep-dist-placeholder",
    apply: "build",
    closeBundle: async () => {
      const distDir = resolve(configDir, "dist");
      await mkdir(distDir, { recursive: true });
      await writeFile(resolve(distDir, ".gitkeep"), "\n");
    },
  };
}

// base: "./" so built asset URLs are relative. Wails serves the embedded dist from
// the app root over the wails:// scheme, where absolute "/assets/..." URLs 404.
export default defineConfig({
  plugins: [react(), stripCrossorigin(), keepDistPlaceholder()],
  base: "./",
  define: { __BUILD_COMMIT__: JSON.stringify(buildCommit()) },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    target: "es2021",
    // Use terser for smaller output (esbuild is faster to build but produces
    // larger bundles). Disabled for dev builds via the default.
    minify: "terser",
    terserOptions: {
      compress: {
        // Keep warn/error so crash breadcrumbs still capture them; drop the noise.
        drop_console: ["log", "debug", "info", "trace"],
        passes: 2,
      },
      // Preserve names so minified crash stacks stay readable.
      keep_classnames: true,
      keep_fnames: true,
    },
    rollupOptions: {
      output: {
        // Manual chunk splitting: keep the heavy markdown/math/code pipeline
        // in a separate chunk so it can be cached independently from the
        // app shell. The vendor chunk splits react+react-dom (stable, rarely
        // changes) from the markdown stack (changes more often).
        manualChunks: {
          "vendor-react": ["react", "react-dom"],
          "vendor-markdown": [
            "react-markdown",
            "remark-gfm",
            "remark-math",
            "rehype-katex",
            "katex",
          ],
          "vendor-highlight": ["highlight.js"],
        },
      },
    },
    // Raise the warning limit — the markdown vendor chunk is legitimately large
    // (katex alone is ~300KB). The manual split ensures it's cached separately.
    chunkSizeWarningLimit: 600,
  },
  server: {
    // Bind IPv4 — unset host listens on ::1, and the Wails dev proxy's [::1]
    // dial fails on Windows hosts where IPv6 loopback is filtered.
    host: "127.0.0.1",
    port: devPort,
    strictPort: true,
  },
});
