#!/usr/bin/env node
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
const explicit = process.argv[2];
const tag = explicit ?? process.env.GITHUB_REF_NAME ?? process.env.TAG;

if (!tag) {
  console.error("usage: sync-desktop-version.mjs <version>  (or set GITHUB_REF_NAME / TAG)");
  process.exit(1);
}

const version = tag.replace(/^v/, "");
if (!/^\d+\.\d+\.\d+(-[\w.-]+)?(\+[\w.-]+)?$/.test(version)) {
  console.error(`refusing to write non-SemVer version: ${version}`);
  process.exit(1);
}

const tauriConfPath = join(repoRoot, "desktop/src-tauri/tauri.conf.json");
const cargoTomlPath = join(repoRoot, "desktop/src-tauri/Cargo.toml");
const desktopPkgPath = join(repoRoot, "desktop/package.json");

const tauriConf = JSON.parse(readFileSync(tauriConfPath, "utf8"));
tauriConf.version = version;
writeFileSync(tauriConfPath, `${JSON.stringify(tauriConf, null, 2)}\n`);

const cargoToml = readFileSync(cargoTomlPath, "utf8");
const versionLine = /^version = ".*"$/m;
if (!versionLine.test(cargoToml)) {
  console.error(`Cargo.toml: no version line matched`);
  process.exit(1);
}
writeFileSync(cargoTomlPath, cargoToml.replace(versionLine, `version = "${version}"`));

const desktopPkg = JSON.parse(readFileSync(desktopPkgPath, "utf8"));
desktopPkg.version = version;
writeFileSync(desktopPkgPath, `${JSON.stringify(desktopPkg, null, 2)}\n`);

console.log(`Synced desktop version → ${version}`);
console.log(`  ${tauriConfPath}`);
console.log(`  ${cargoTomlPath}`);
console.log(`  ${desktopPkgPath}`);
