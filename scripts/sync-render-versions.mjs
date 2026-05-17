#!/usr/bin/env node
// Sync version + optionalDependencies pins across the main package.json
// and every subpackages/render-*/package.json. Run on every version bump
// (and in CI before publish so a stale subpackage doesn't ship pinned to
// an old main version).
//
// Usage:
//   node scripts/sync-render-versions.mjs                  # sync to main package.json's version
//   node scripts/sync-render-versions.mjs 0.44.0           # bump main + subpackages to the given version
//   node scripts/sync-render-versions.mjs --check          # exit 1 if anything's out of sync (CI guard)

import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const mainPkgPath = join(root, "package.json");
const subpackagesDir = join(root, "subpackages");

const args = process.argv.slice(2).filter((a) => a.length > 0);
const check = args.includes("--check");
const explicit = args.find((a) => !a.startsWith("--"));

const mainPkg = readJson(mainPkgPath);
const targetVersion = explicit ?? mainPkg.version;
if (!/^\d+\.\d+\.\d+/.test(targetVersion)) {
  exitErr(`bad version: ${targetVersion}`);
}

const subNames = readdirSync(subpackagesDir).filter((n) => n.startsWith("render-"));
const subPkgs = subNames.map((n) => {
  const path = join(subpackagesDir, n, "package.json");
  return { name: n, path, pkg: readJson(path) };
});

const expectedOptionalDeps = Object.fromEntries(
  subPkgs.map((s) => [s.pkg.name, targetVersion]).sort(([a], [b]) => a.localeCompare(b)),
);

if (check) {
  const drift = [];
  if (mainPkg.version !== targetVersion) {
    drift.push(`main package.json version ${mainPkg.version} != ${targetVersion}`);
  }
  const mainOpt = mainPkg.optionalDependencies ?? {};
  for (const [name, pinned] of Object.entries(expectedOptionalDeps)) {
    if (mainOpt[name] !== pinned) {
      drift.push(`main optionalDependencies['${name}'] = ${mainOpt[name]} != ${pinned}`);
    }
  }
  for (const s of subPkgs) {
    if (s.pkg.version !== targetVersion) {
      drift.push(`${s.name}/package.json version ${s.pkg.version} != ${targetVersion}`);
    }
  }
  if (drift.length > 0) {
    process.stderr.write(`✗ render-version sync drift:\n  ${drift.join("\n  ")}\n`);
    process.exit(1);
  }
  process.stderr.write(`✓ render versions in sync (${targetVersion})\n`);
  process.exit(0);
}

mainPkg.version = targetVersion;
mainPkg.optionalDependencies = expectedOptionalDeps;
writeJson(mainPkgPath, mainPkg);

for (const s of subPkgs) {
  s.pkg.version = targetVersion;
  writeJson(s.path, s.pkg);
}

process.stderr.write(`✓ synced ${subPkgs.length} subpackages + main to ${targetVersion}\n`);

function readJson(path) {
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch (err) {
    exitErr(`read ${path}: ${err.message}`);
  }
}

function writeJson(path, obj) {
  writeFileSync(path, `${JSON.stringify(obj, null, 2)}\n`);
}

function exitErr(msg) {
  process.stderr.write(`✗ ${msg}\n`);
  process.exit(1);
}
