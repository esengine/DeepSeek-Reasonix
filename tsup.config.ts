import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "tsup";

const here = fileURLToPath(new URL(".", import.meta.url));
// Resolve "ink" imports inside the CLI bundle to our cell-diff renderer's
// shim. The real "ink" package never makes it into dist/cli; everything that
// imports Box / Text / Static / useApp / useInput / useStdout / render flows
// through src/renderer/ink-compat. Library + dashboard bundles are unchanged.
const inkCompatPath = resolve(here, "src/renderer/ink-compat/index.ts");

export default defineConfig([
  {
    entry: ["src/index.ts"],
    format: ["esm"],
    dts: true,
    clean: true,
    sourcemap: true,
    target: "node22",
    outDir: "dist",
  },
  {
    entry: ["src/cli/index.ts"],
    format: ["esm"],
    dts: false,
    clean: false,
    sourcemap: true,
    target: "node22",
    outDir: "dist/cli",
    banner: { js: "#!/usr/bin/env node" },
    // Force the CLI bundle to inline "ink" so the alias actually fires.
    // Without noExternal, tsup keeps "ink" as a literal external import and
    // node would resolve it to the real package at runtime.
    noExternal: ["ink"],
    esbuildOptions(options) {
      options.alias = { ...(options.alias ?? {}), ink: inkCompatPath };
    },
  },
  {
    entry: { app: "dashboard/app.js" },
    format: ["esm"],
    dts: false,
    clean: true,
    sourcemap: true,
    target: "es2022",
    platform: "browser",
    outDir: "dashboard/dist",
    noExternal: [/.*/],
    splitting: false,
  },
]);
