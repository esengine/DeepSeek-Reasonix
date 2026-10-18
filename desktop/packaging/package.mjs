#!/usr/bin/env node
// Builds the UI for Electron, builds the shell and packages both with
// @electron/packager into desktop/build/electron/<os>-<arch>/. The Go binaries
// are added afterwards by scripts/desktop-build.sh, which owns signing and the
// per-platform artifacts.
//
// usage: node desktop/packaging/package.mjs <os/arch> <version> [channel]
import { defaultSanitizePackageJson, packager } from "@electron/packager";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { chmodSync, cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join } from "node:path";
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
  walkFiles
} from "./lib.mjs";
import { frontendProducerAttempt, verifyFrontendArtifact } from "../frontend/scripts/artifact-identity.mjs";

const desktop = dirname(dirname(fileURLToPath(import.meta.url)));
const repo = dirname(desktop);
const mcpBridge = join(repo, "mcp-bridge.js");
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
    return execFileSync("git", ["-C", repo, "rev-parse", "HEAD"], { encoding: "utf8", }).trim();
  } catch {
    return "unknown";
  }
}

function sha256File(path) {
  return createHash("sha256").update(readFileSync(path)).digest("hex");
}

function require(path, what) {
  if (!existsSync(path)) throw new Error(`${what} is missing: ${path}`);
}

const frontendDist = join(desktop, "frontend", "dist");
require(mcpBridge, "Chrome MCP bridge");
if (process.env.REASONIX_PACKAGE_REUSE_FRONTEND === "1") {
  const pnpmVersion = (process.env.REASONIX_FRONTEND_PNPM_VERSION ?? "").trim();
  if (!pnpmVersion) throw new Error("REASONIX_FRONTEND_PNPM_VERSION is required when reusing a frontend artifact");
  verifyFrontendArtifact({
    root: repo,
    dist: frontendDist,
    manifest: process.env.REASONIX_FRONTEND_ARTIFACT_MANIFEST || join(desktop, "frontend", ".reasonix-frontend-artifact.json"),
    shell: "electron",
    channel,
    sourceSHA: process.env.GITHUB_SHA || undefined,
    runId: process.env.GITHUB_RUN_ID || undefined,
    attempt: frontendProducerAttempt(),
    pnpmVersion,
  });
  console.log(`==> reusing ${frontendDist}`);
} else {
  console.log(`==> frontend build:electron (channel ${channel})`);
  runBuildScript(join(desktop, "frontend"), "build-for-shell.mjs", ["electron"], { REASONIX_CHANNEL: channel, REASONIX_COMMIT: commit });
}
require(join(frontendDist, "index.html"), "frontend dist");

console.log("==> shell build");
runBuildScript(join(desktop, "electron"), "build.mjs");
const shellDist = join(desktop, "electron", "dist");
for (const name of ["main.cjs", "preload.cjs", "browser-page.js", "browser-page.json", "browser-playwright-LICENSE", "browser-playwright-NOTICE", "browser-recorder.js", "recorder-preload.cjs"]) require(join(shellDist, name), "shell bundle");
if (!existsSync(join(shellDist, "desktopContract.json")) && process.env.REASONIX_ELECTRON_ALLOW_MISSING_CONTRACT !== "1") {
  throw new Error(`desktop contract is missing from ${shellDist}; run: cd desktop && go run . -emit-contract frontend/src/generated`);
}

const sourceMapDir = join(desktop, "build", "sourcemaps", target.key);
rmSync(sourceMapDir, { recursive: true, force: true });
mkdirSync(sourceMapDir, { recursive: true });
for (const name of readdirSync(shellDist).filter((name) => name.endsWith(".map"))) {
  cpSync(join(shellDist, name), join(sourceMapDir, name));
}
const frontendMapArchive = join(desktop, "frontend", "sourcemaps", commit);
require(join(frontendMapArchive, "manifest.json"), "frontend source map archive manifest");
const frontendMapManifest = JSON.parse(readFileSync(join(frontendMapArchive, "manifest.json"), "utf8"));
if (frontendMapManifest.commit !== commit) throw new Error(`frontend source map commit ${frontendMapManifest.commit} does not match ${commit}`);
if (!Array.isArray(frontendMapManifest.maps) || frontendMapManifest.maps.length === 0) throw new Error("frontend source map archive manifest has no maps");
for (const record of frontendMapManifest.maps) {
  if (!record || typeof record.archive !== "string" || typeof record.map !== "string" || typeof record.bundle !== "string") {
    throw new Error("frontend source map archive manifest has an invalid map record");
  }
  if (
    !record.map.endsWith(".map") ||
    record.map.startsWith("/") ||
    record.map.includes("\\") ||
    record.map.split("/").includes("..") ||
    record.archive.includes("/") ||
    record.archive.includes("\\") ||
    record.archive.includes("..") ||
    record.bundle !== record.map.slice(0, -4)
  ) {
    throw new Error(`frontend source map archive manifest has an unsafe map record: ${JSON.stringify(record)}`);
  }
  const archivedMap = join(frontendMapArchive, record.archive);
  require(archivedMap, `archived frontend source map ${record.map}`);
  const destination = join(sourceMapDir, "frontend", record.map);
  mkdirSync(dirname(destination), { recursive: true });
  cpSync(archivedMap, destination);
}

const sourceMapEntries = [];
for (const mapPath of walkFiles(sourceMapDir)
  .filter((name) => name.endsWith(".map"))
  .sort()) {
  const shellMap = !mapPath.startsWith("frontend/");
  const bundleRelative = shellMap ? mapPath.slice(0, -4) : mapPath.slice("frontend/".length, -4);
  if (!bundleRelative || bundleRelative.includes("..")) throw new Error(`source map has an invalid bundle path: ${mapPath}`);
  const mapJSON = JSON.parse(readFileSync(join(sourceMapDir, mapPath), "utf8"));
  if (mapJSON.file && String(mapJSON.file) !== basename(bundleRelative)) throw new Error(`source map bundle identity does not match ${mapPath}`);
  const bundlePath = shellMap ? join(shellDist, bundleRelative) : join(frontendDist, bundleRelative);
  require(bundlePath, `bundle for source map ${mapPath}`);
  sourceMapEntries.push({
    bundle: shellMap ? `electron/${bundleRelative}` : bundleRelative,
    bundleHash: `sha256:${sha256File(bundlePath)}`,
    map: mapPath,
    mapHash: `sha256:${sha256File(join(sourceMapDir, mapPath))}`,
  });
}
for (const requiredMap of ["main.cjs.map", "preload.cjs.map"]) {
  if (!sourceMapEntries.some((entry) => entry.map === requiredMap)) throw new Error(`required Electron source map is missing: ${requiredMap}`);
}
if (!sourceMapEntries.some((entry) => entry.map.startsWith("frontend/"))) throw new Error("frontend source maps are missing");
writeFileSync(
  join(sourceMapDir, "manifest.json"),
  JSON.stringify(
    {
      schemaVersion: 1,
      commit: (process.env.GITHUB_SHA ?? "").trim() || gitCommit(),
      target: target.spec,
      channel,
      createdAt: buildTime,
      entries: sourceMapEntries,
    },
    null,
    2,
  ) + "\n",
);

const staging = mkdtempSync(join(tmpdir(), "reasonix-package-"));
const outDir = join(desktop, "build", "electron", target.key);
try {
  cpSync(frontendDist, join(staging, "app"), {
    recursive: true,
    filter: (source) => !source.endsWith(".map"),
  });
  mkdirSync(join(staging, "icons"), { recursive: true });
  cpSync(join(desktop, "build", "appicon.png"), join(staging, "icons", "appicon.png"));
  cpSync(join(desktop, "build", "trayTemplate.png"), join(staging, "icons", "trayTemplate.png"));
  cpSync(join(desktop, "build", "trayTemplate@2x.png"), join(staging, "icons", "trayTemplate@2x.png"));
  // Packaged launches always read this identity, including the full version
  // tag. Environment overrides belong only to the unpackaged development shell.
  writeFileSync(join(staging, "build.json"), JSON.stringify(buildInfo({ version, channel, commit, electronVersion, target, buildTime, }), null, 2,) + "\n",);

  const icon = { darwin: join(desktop, "build", "darwin", "icon.icns"), win32: join(desktop, "build", "windows", "icon.ico"), }[target.packagerPlatform];
  if (icon) require(icon, "application icon");
  const options = packagerOptions({
    target,
    version,
    identity,
    root: desktop,
    electronVersion,
    extraResources: [join(staging, "app"), join(staging, "icons"), join(staging, "build.json"), mcpBridge],
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
  // The packager stages the app tree in a mkdtemp directory (0700) and renames
  // it into place; dpkg installs that mode as root:root, hiding app/ from users.
  if (target.os !== "darwin") chmodSync(bundle, 0o755);
  rmSync(options.out, { recursive: true, force: true });

  if (target.os === "windows") {
    const installer = join(desktop, "build", "windows", "installer");
    mkdirSync(installer, { recursive: true });
    writeFileSync(join(installer, "reasonix_project.nsh"), nsisProjectDefines(identity, version));
    const signing = signingFileList(walkFiles(bundle).map((name) => `app/${name}`));
    writeFileSync(join(outDir, "signing-files.txt"), signing.join("\n") + "\n");
    console.log(`==> ${signing.length} Electron PE files need Authenticode (${join(outDir, "signing-files.txt")})`);
  }
  writeFileSync(join(outDir, "summary.json"), JSON.stringify({ target: target.spec, version, channel, commit, electronVersion, bundle, }, null, 2,) + "\n",);
  console.log(`==> packaged ${bundle}`);
} finally {
  rmSync(staging, { recursive: true, force: true });
}
