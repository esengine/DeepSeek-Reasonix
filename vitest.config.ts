import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const here = fileURLToPath(new URL(".", import.meta.url));
// Mirror tsup's CLI alias so tests run through the same renderer shim that
// production ships. The real "ink" package is no longer a runtime dep.
const inkCompatPath = resolve(here, "src/renderer/ink-compat/index.ts");

export default defineConfig({
  resolve: {
    alias: {
      ink: inkCompatPath,
      "@": resolve(here, "src"),
    },
  },
  test: {
    include: ["tests/**/*.test.ts", "tests/**/*.test.tsx"],
    setupFiles: ["tests/setup-lang.ts"],
    environment: "node",
    globals: false,
    // One retry absorbs Windows scheduler hiccups in jobs.test.ts / loop.test.ts /
    // bundle-smoke (real spawns + tokenizer cold load). A real failure still re-fails.
    retry: 1,
    coverage: {
      provider: "v8",
      reporter: ["text", "html", "json-summary"],
      include: ["src/**"],
      exclude: ["src/cli/ui/**", "src/**/*.test.ts"],
    },
  },
});
