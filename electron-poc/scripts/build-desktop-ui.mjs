#!/usr/bin/env node
/**
 * Build desktop/frontend into electron-poc/desktop-ui for the Electron shell.
 */
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = path.resolve(root, "..");
const frontendDir = path.join(repoRoot, "desktop", "frontend");
const outDir = path.join(root, "desktop-ui");
const ifMissing = process.argv.includes("--if-missing");

if (ifMissing && fs.existsSync(path.join(outDir, "index.html"))) {
  console.log("desktop-ui already present, skip build");
  process.exit(0);
}

if (!fs.existsSync(frontendDir)) {
  console.error("desktop/frontend not found at", frontendDir);
  process.exit(1);
}

console.log("building desktop frontend → electron-poc/desktop-ui …");
// vite build only (skip full lint suite) for PoC iteration speed
const r = spawnSync(
  "pnpm",
  ["exec", "vite", "build", "--outDir", outDir, "--emptyOutDir"],
  {
    cwd: frontendDir,
    stdio: "inherit",
    env: {
      ...process.env,
      REASONIX_CHANNEL: "electron-poc",
    },
  },
);
if (r.status !== 0) {
  // fallback npm exec
  const r2 = spawnSync(
    "npx",
    ["vite", "build", "--outDir", outDir, "--emptyOutDir"],
    {
      cwd: frontendDir,
      stdio: "inherit",
      env: { ...process.env, REASONIX_CHANNEL: "electron-poc" },
    },
  );
  if (r2.status !== 0) process.exit(r2.status || 1);
}

if (!fs.existsSync(path.join(outDir, "index.html"))) {
  console.error("build finished but index.html missing in", outDir);
  process.exit(1);
}
console.log("desktop-ui ready:", outDir);
