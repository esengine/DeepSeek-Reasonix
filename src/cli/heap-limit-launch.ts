/** Importing this module re-execs the process with `--max-old-space-size=<target>` when Node's stock 2 GiB cap is in force (issue #1011). Must be the first import in src/cli/index.ts so the child inherits a bigger heap before commander / clients / dashboard server load. No-op when the user already passed --max-old-space-size, set NODE_OPTIONS, or the system memory wouldn't comfortably support the raise. */

import { spawnSync } from "node:child_process";
import { totalmem } from "node:os";
import { fileURLToPath } from "node:url";
import { getHeapStatistics } from "node:v8";
import { RX_HEAP_REEXEC_ENV, decideHeapTargetMb } from "./heap-limit.js";

const runningUnderVitest =
  process.env.VITEST === "true" || process.env.VITEST_WORKER_ID !== undefined;

const target = decideHeapTargetMb({
  currentLimitMb: Math.floor(getHeapStatistics().heap_size_limit / 1024 / 1024),
  totalMemMb: Math.floor(totalmem() / 1024 / 1024),
  nodeOptions: process.env.NODE_OPTIONS ?? "",
  execArgv: process.execArgv,
  alreadyReexec: process.env[RX_HEAP_REEXEC_ENV] === "1" || runningUnderVitest,
});

if (target !== null) {
  const existing = process.env.NODE_OPTIONS ?? "";
  const nextOptions = `${existing} --max-old-space-size=${target}`.trim();
  const childEnv = { ...process.env, NODE_OPTIONS: nextOptions, [RX_HEAP_REEXEC_ENV]: "1" };
  const extension = import.meta.url.endsWith(".ts") ? "ts" : "js";
  const entrypoint = fileURLToPath(new URL(`./index.${extension}`, import.meta.url));
  const result = spawnSync(
    process.execPath,
    [...process.execArgv, entrypoint, ...process.argv.slice(2)],
    {
      env: childEnv,
      stdio: "inherit",
    },
  );
  process.exit(result.status ?? 0);
}
