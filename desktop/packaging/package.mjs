#!/usr/bin/env node
// Builds the UI for Electron, builds the shell and packages both with
// @electron/packager into desktop/build/electron/<os>-<arch>/. The Go binaries
// are added afterwards by scripts/desktop-build.sh, which owns signing and the
// per-platform artifacts.
//
// usage: node desktop/packaging/package.mjs <os/arch> <version> [channel]
import { defaultSanitizePackageJson, packager } from "@electron/packager";
import { execFileSync } from "node:child_process";
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import {
  buildInfo,
  nsisProjectDefines,
  packagerOptions,
  parseTarget,
  PRODUCT,
  readProductIdentity,
  runBuildScript,
  sanitizeShellPackageJson,
  signingFileList,
  versionTag,
  walkFiles,
} from "./lib.mjs";

const desktop = dirname(dirname(fileURLToPath(import.meta.url)));
const repo = dirname(desktop);
const [spec, version, channel = "stable"] = process.argv.slice(2);
if (!spec || !version) {
  console.error("usage: package.mjs <os/arch> <version> [channel]");
  process.exit(2);
}
const target = parseTarget(spec);
versionTag(version);
const identity = readProductIdentity();
const electronVersion = JSON.parse(readFileSync(join(desktop, "electron", "node_modules", "electron", "package.json"), "utf8")).version;
const commit = (process.env.REASONIX_COMMIT ?? "").trim() || gitCommit();
const buildTime = (process.env.REASONIX_BUILD_TIME ?? "").trim() || new Date().toISOString().replace(/\.\d{3}Z$/, "Z");

function gitCommit() {
  try {
    return execFileSync("git", ["-C", repo, "rev-parse", "--short=12", "HEAD"], { encoding: "utf8" }).trim();
  } catch {
    return "unknown";
  }
}

function require(path, what) {
  if (!existsSync(path)) throw new Error(`${what} is missing: ${path}`);
}

const frontendDist = join(desktop, "frontend", "dist");
if (process.env.REASONIX_PACKAGE_REUSE_FRONTEND === "1" && existsSync(join(frontendDist, "index.html"))) {
  console.log(`==> reusing ${frontendDist}`);
} else {
  console.log(`==> frontend build:electron (channel ${channel})`);
  runBuildScript(join(desktop, "frontend"), "build-for-shell.mjs", ["electron"], { REASONIX_CHANNEL: channel });
}
require(join(frontendDist, "index.html"), "frontend dist");

console.log("==> shell build");
runBuildScript(join(desktop, "electron"), "build.mjs");
const shellDist = join(desktop, "electron", "dist");
for (const name of ["main.cjs", "preload.cjs"]) require(join(shellDist, name), "shell bundle");
if (!existsSync(join(shellDist, "desktopContract.json")) && process.env.REASONIX_ELECTRON_ALLOW_MISSING_CONTRACT !== "1") {
  throw new Error(`desktop contract is missing from ${shellDist}; run: cd desktop && go run . -emit-contract frontend/src/generated`);
}

const staging = mkdtempSync(join(tmpdir(), "reasonix-package-"));
const outDir = join(desktop, "build", "electron", target.key);
try {
  cpSync(frontendDist, join(staging, "app"), { recursive: true });
  mkdirSync(join(staging, "icons"), { recursive: true });
  cpSync(join(desktop, "build", "appicon.png"), join(staging, "icons", "appicon.png"));
  cpSync(join(desktop, "build", "linux", "icons"), join(staging, "icons", "linux", "icons"), { recursive: true });
  // Packaged launches always read this identity, including the full version
  // tag. Environment overrides belong only to the unpackaged development shell.
  writeFileSync(join(staging, "build.json"), JSON.stringify(buildInfo({ version, channel, commit, electronVersion, target, buildTime }), null, 2) + "\n");

  const icon = { darwin: join(desktop, "build", "darwin", "icon.icns"), win32: join(desktop, "build", "windows", "icon.ico") }[target.packagerPlatform];
  if (icon) require(icon, "application icon");
  const options = packagerOptions({
    target,
    version,
    identity,
    root: desktop,
    electronVersion,
    extraResources: [join(staging, "app"), join(staging, "icons"), join(staging, "build.json")],
    icon,
  });
  options.sanitizePackageJson = [defaultSanitizePackageJson, (pkg) => sanitizeShellPackageJson(pkg, { version, productName: PRODUCT.name })];
  rmSync(options.out, { recursive: true, force: true });
  rmSync(outDir, { recursive: true, force: true });

  console.log(`==> packaging ${PRODUCT.name} ${version} for ${target.spec} with Electron ${electronVersion}`);
  const [finalPath] = await packager(options);
  mkdirSync(outDir, { recursive: true });
  const bundle = target.os === "darwin" ? join(outDir, `${PRODUCT.name}.app`) : join(outDir, "app");
  renameSync(target.os === "darwin" ? join(finalPath, `${PRODUCT.name}.app`) : finalPath, bundle);
  rmSync(options.out, { recursive: true, force: true });

  if (target.os === "windows") {
    const installer = join(desktop, "build", "windows", "installer");
    mkdirSync(installer, { recursive: true });
    writeFileSync(join(installer, "reasonix_project.nsh"), nsisProjectDefines(identity, version));
    const signing = signingFileList(walkFiles(bundle).map((name) => `app/${name}`));
    writeFileSync(join(outDir, "signing-files.txt"), signing.join("\n") + "\n");
    console.log(`==> ${signing.length} Electron PE files need Authenticode (${join(outDir, "signing-files.txt")})`);
  }
  writeFileSync(join(outDir, "summary.json"), JSON.stringify({ target: target.spec, version, channel, commit, electronVersion, bundle }, null, 2) + "\n");
  console.log(`==> packaged ${bundle}`);
} finally {
  rmSync(staging, { recursive: true, force: true });
}
