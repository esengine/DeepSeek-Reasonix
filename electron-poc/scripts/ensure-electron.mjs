#!/usr/bin/env node
/**
 * Ensure Electron dist + path.txt exist (npm allow-scripts can skip postinstall).
 * Extracts from @electron/get cache when present, otherwise downloads.
 */
import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const electronDir = path.join(root, "node_modules", "electron");
const distDir = path.join(electronDir, "dist");
const pathTxt = path.join(electronDir, "path.txt");

function platformPath() {
  if (process.platform === "darwin") return "Electron.app/Contents/MacOS/Electron";
  if (process.platform === "win32") return "electron.exe";
  return "electron";
}

function isReady() {
  if (!fs.existsSync(pathTxt)) return false;
  const rel = fs.readFileSync(pathTxt, "utf8").trim();
  const abs = path.join(distDir, rel);
  return fs.existsSync(abs);
}

async function main() {
  if (!fs.existsSync(electronDir)) {
    console.error("electron package missing; run npm install in electron-poc first");
    process.exit(1);
  }
  if (isReady()) {
    console.log("electron dist ready:", fs.readFileSync(pathTxt, "utf8").trim());
    return;
  }

  const pkg = JSON.parse(fs.readFileSync(path.join(electronDir, "package.json"), "utf8"));
  const version = pkg.version;
  let zipPath;
  try {
    const { downloadArtifact } = require(path.join(electronDir, "node_modules", "@electron/get"));
    // @electron/get may live at top-level node_modules
    zipPath = await downloadArtifact({
      version,
      artifactName: "electron",
      platform: process.platform,
      arch: process.arch,
    });
  } catch {
    const { downloadArtifact } = require("@electron/get");
    zipPath = await downloadArtifact({
      version,
      artifactName: "electron",
      platform: process.platform,
      arch: process.arch,
    });
  }
  console.log("downloaded", zipPath);
  fs.rmSync(distDir, { recursive: true, force: true });
  fs.mkdirSync(distDir, { recursive: true });
  const unzip = spawnSync("unzip", ["-q", zipPath, "-d", distDir], { encoding: "utf8" });
  if (unzip.status !== 0) {
    // Windows fallback: powershell Expand-Archive not handled; try node extract via unzip error
    console.error(unzip.stderr || unzip.stdout);
    process.exit(1);
  }
  fs.writeFileSync(pathTxt, platformPath());
  if (!isReady()) {
    console.error("electron still not ready after extract");
    process.exit(1);
  }
  console.log("electron dist ready:", platformPath());
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
