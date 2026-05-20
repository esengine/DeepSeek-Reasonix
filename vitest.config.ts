import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const here = fileURLToPath(new URL(".", import.meta.url));

export default defineConfig({
  resolve: {
    alias: {
      "@": resolve(here, "src"),
    },
  },
  test: {
    include: ["tests/**/*.test.ts", "tests/**/*.test.tsx"],
    setupFiles: ["tests/setup-lang.ts"],
    environment: "node",
    globals: false,
    // Forks pool — per-file process isolation, so tokenizer BPE / tree-sitter
    // wasms / sqlite native handles can't accumulate in a single shared heap.
    // Threads default OOMs on 16-core boxes where 15 workers × ~300MB blows
    // past Node's 4GB heap cap.
    pool: "forks",
    poolOptions: {
      forks: { maxForks: 8, minForks: 1 },
    },
    // One retry absorbs Windows scheduler hiccups in jobs.test.ts / loop.test.ts /
    // bundle-smoke (real spawns + tokenizer cold load). A real failure still re-fails.
    retry: 1,
    coverage: {
      provider: "v8",
      reporter: ["text", "html", "json-summary"],
      include: ["src/**"],
      exclude: ["src/**/*.test.ts"],
    },
  },
});
