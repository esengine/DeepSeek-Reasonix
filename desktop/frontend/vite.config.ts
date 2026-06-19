import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import { execSync } from "node:child_process";
import { mkdir, readdir, rename, writeFile } from "node:fs/promises";
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

function buildChannel(): string {
  return process.env.REASONIX_CHANNEL || "stable";
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

function archiveHiddenSourcemaps(commit: string): Plugin {
  async function collectMapFiles(dir: string): Promise<string[]> {
    const entries = await readdir(dir, { withFileTypes: true }).catch(() => []);
    const files: string[] = [];
    for (const entry of entries) {
      const p = resolve(dir, entry.name);
      if (entry.isDirectory()) files.push(...(await collectMapFiles(p)));
      else if (entry.isFile() && entry.name.endsWith(".map")) files.push(p);
    }
    return files;
  }

  return {
    name: "archive-hidden-sourcemaps",
    apply: "build",
    closeBundle: async () => {
      const distDir = resolve(configDir, "dist");
      const maps = await collectMapFiles(distDir);
      if (!maps.length) return;

      const archiveDir = resolve(configDir, "sourcemaps", commit);
      await mkdir(archiveDir, { recursive: true });
      await Promise.all(
        maps.map(async (mapPath) => {
          const rel = mapPath.slice(distDir.length + 1).replace(/[\\/]+/g, "__");
          await rename(mapPath, resolve(archiveDir, rel));
        }),
      );
      await writeFile(
        resolve(archiveDir, "manifest.json"),
        JSON.stringify({ commit, channel: buildChannel(), archivedAt: new Date().toISOString() }, null, 2) + "\n",
      );
    },
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

const commit = buildCommit();
const channel = buildChannel();

const nodeModulePath = String.raw`[\\/]node_modules[\\/](?:\.pnpm[\\/][^\\/]+[\\/]node_modules[\\/])?`;
const vendorReact = new RegExp(`${nodeModulePath}(?:react|react-dom)(?:[\\/]|$)`);
const vendorMarkdown = new RegExp(
  `${nodeModulePath}(?:react-markdown|remark-gfm|remark-math|rehype-katex|katex)(?:[\\/]|$)`,
);
const vendorHighlight = new RegExp(`${nodeModulePath}highlight\\.js(?:[\\/]|$)`);

// base: "./" so built asset URLs are relative. Wails serves the embedded dist from
// the app root over the wails:// scheme, where absolute "/assets/..." URLs 404.
export default defineConfig({
  // errorRecovery tells lightningcss to skip unparseable rules instead of
  // failing the whole build. Vite 8 + lightningcss 1.32.0 can reject valid
  // @keyframes in concatenated CSS bundles (heartbeat.css + styles.css).
  css: {
    lightningcss: { errorRecovery: true },
  },
  plugins: [react(), stripCrossorigin(), archiveHiddenSourcemaps(commit), keepDistPlaceholder()],
  base: "./",
  define: { __BUILD_COMMIT__: JSON.stringify(commit), __BUILD_CHANNEL__: JSON.stringify(channel) },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: "hidden",
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
    rolldownOptions: {
      output: {
        // Manual chunk splitting: keep the heavy markdown/math/code pipeline
        // in a separate chunk so it can be cached independently from the
        // app shell. The vendor chunk splits react+react-dom (stable, rarely
        // changes) from the markdown stack (changes more often).
        codeSplitting: {
          groups: [
            { name: "vendor-react", test: vendorReact },
            { name: "vendor-markdown", test: vendorMarkdown },
            { name: "vendor-highlight", test: vendorHighlight },
          ],
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
