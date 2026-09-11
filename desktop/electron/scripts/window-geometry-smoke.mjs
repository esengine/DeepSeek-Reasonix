import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const require = createRequire(import.meta.url);
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const scratch = mkdtempSync(join(tmpdir(), "reasonix-window-geometry-"));
try {
  const entry = join(scratch, "main.cjs");
  await build({ entryPoints: [join(root, "src/main/windowGeometry.native.ts")], outfile: entry,
    bundle: true, platform: "node", format: "cjs", external: ["electron"] });
  const env = { ...process.env };
  delete env.ELECTRON_RUN_AS_NODE;
  execFileSync(require("electron"), [entry, `--user-data-dir=${join(scratch, "data")}`],
    { env, stdio: "inherit", timeout: 60_000 });
} finally {
  rmSync(scratch, { recursive: true, force: true });
}
