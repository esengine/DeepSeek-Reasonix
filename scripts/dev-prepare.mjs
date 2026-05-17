#!/usr/bin/env node
// Build the rust renderer as a native Node addon (.node) before tsx hands
// off to `tsx src/cli/index.ts`. Silent no-op when cargo isn't installed
// (TS-only contributors fall back to the Ink TUI) or when the source tree
// isn't present (published install — the optional-dep ships the prebuilt
// .node). The loader at src/cli/ui/scene/renderer.ts looks here first
// before falling through to `@reasonix/render-<platform>-<arch>`.

import { spawnSync } from "node:child_process";
import { copyFileSync, existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

const cargoCheck = spawnSync("cargo", ["--version"], { stdio: "ignore" });
if (cargoCheck.status !== 0) {
  process.exit(0);
}

const build = spawnSync(
  "cargo",
  [
    "build",
    "--release",
    "--quiet",
    "--lib",
    "--features",
    "napi",
    "--manifest-path",
    "crates/reasonix-render/Cargo.toml",
  ],
  { stdio: "inherit", cwd: root },
);

if (build.status !== 0) {
  process.stderr.write(
    "▲ cargo build for reasonix-render lib failed — Node will fall back to the Ink TUI.\n",
  );
  process.exit(0);
}

const triple = tripleFor(process.platform, process.arch);
if (!triple) {
  process.stderr.write(
    `▲ unsupported platform ${process.platform}-${process.arch}; skipping .node copy.\n`,
  );
  process.exit(0);
}

const built = join(root, "target", "release", dylibName(process.platform));
const dest = join(root, "crates", "reasonix-render", `reasonix-render.${triple}.node`);
if (!existsSync(built)) {
  process.stderr.write(`▲ expected ${built} after cargo build — skipping.\n`);
  process.exit(0);
}
copyFileSync(built, dest);
process.exit(0);

function tripleFor(platform, arch) {
  if (platform === "win32" && arch === "x64") return "win32-x64-msvc";
  if (platform === "win32" && arch === "arm64") return "win32-arm64-msvc";
  if (platform === "darwin" && arch === "arm64") return "darwin-arm64";
  if (platform === "darwin" && arch === "x64") return "darwin-x64";
  if (platform === "linux" && arch === "x64") return "linux-x64-gnu";
  if (platform === "linux" && arch === "arm64") return "linux-arm64-gnu";
  return null;
}

function dylibName(platform) {
  if (platform === "win32") return "reasonix_render.dll";
  if (platform === "darwin") return "libreasonix_render.dylib";
  return "libreasonix_render.so";
}
