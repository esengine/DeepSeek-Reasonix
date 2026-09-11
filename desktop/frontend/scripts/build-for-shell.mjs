#!/usr/bin/env node
// Runs the frontend build for one shell. The shell name travels through the
// environment because npm scripts cannot set variables portably on Windows.
import { spawnSync } from "node:child_process";
import { shellFromEnv } from "./shell-css.mjs";

const shell = shellFromEnv({ REASONIX_SHELL: process.argv[2] ?? "" });
const result = spawnSync("pnpm", ["build"], {
  stdio: "inherit",
  env: { ...process.env, REASONIX_SHELL: shell },
  shell: process.platform === "win32",
});
process.exit(result.status ?? 1);
