import { readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const VERSION_RE = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/;
const SHA_RE = /^[0-9a-f]{40}$/;

function compareStable(a, b) {
  if (!VERSION_RE.test(a) || !VERSION_RE.test(b)) throw new Error("invalid stable version in publication observation");
  const aa = a.split(".").map(Number);
  const bb = b.split(".").map(Number);
  for (let index = 0; index < aa.length; index += 1) {
    if (aa[index] !== bb[index]) return aa[index] > bb[index] ? 1 : -1;
  }
  return 0;
}

export function ownsPublicSite(version, operation, manifest) {
  if (!["publish", "recover"].includes(operation)) throw new Error("invalid publication operation");
  const current = manifest?.version;
  if (typeof current !== "string" || !current.startsWith("v")) throw new Error("missing or invalid Stable manifest version");
  const comparison = compareStable(current.slice(1), version);
  if (comparison === 0) return true;
  if (comparison > 0 && operation === "recover") return false;
  throw new Error(`Stable manifest serves ${current}, want v${version}`);
}

function requireIdentity(version, sourceSHA, operation) {
  if (!VERSION_RE.test(version)) throw new Error("invalid publication ledger version");
  if (!SHA_RE.test(sourceSHA)) throw new Error("invalid publication ledger source SHA");
  if (!["publish", "recover"].includes(operation)) throw new Error("invalid publication operation");
}

function releaseAssets(release, surface) {
  if (release?.isDraft !== false || release?.isPrerelease !== false || !Array.isArray(release.assets)) {
    throw new Error(`${surface} release is not a public final release`);
  }
  return release.assets.map(asset => ({
    name: asset.name,
    size: asset.size,
    digest: asset.digest || null,
    state: "identity-verified",
  })).sort((a, b) => a.name.localeCompare(b.name));
}

export function createCoreLedger({ version, sourceSHA, operation, cliRelease, desktopRelease, observedAt = new Date().toISOString() }) {
  requireIdentity(version, sourceSHA, operation);
  return {
    schema: 1,
    version,
    sourceSHA,
    operation,
    observedAt,
    surfaces: {
      tags: {
        state: "identity-verified",
        items: [`v${version}`, `npm-v${version}`, `desktop-v${version}`].map(name => ({ name, sha: sourceSHA })),
      },
      cli: { state: "identity-verified", assets: releaseAssets(cliRelease, "CLI") },
      desktop: { state: "identity-verified", assets: releaseAssets(desktopRelease, "Desktop") },
    },
  };
}

export function createSiteLedger({ version, sourceSHA, operation, manifest, observedAt = new Date().toISOString() }) {
  requireIdentity(version, sourceSHA, operation);
  if (manifest?.version !== `v${version}`) throw new Error("Stable manifest does not match the publication ledger");
  return {
    schema: 1,
    version,
    sourceSHA,
    operation,
    observedAt,
    surfaces: {
      stableManifest: { state: "public-entry-updated", version: manifest.version },
      homepage: { state: "public-entry-updated", version: `v${version}` },
      changelog: { state: "public-entry-updated", version: `v${version}` },
    },
  };
}

export function mergeLedgers(core, site, observedAt = new Date().toISOString()) {
  if (core.schema !== 1 || site.schema !== 1 || core.version !== site.version
      || core.sourceSHA !== site.sourceSHA || core.operation !== site.operation) {
    throw new Error("publication ledger fragments do not describe one release");
  }
  return { ...core, observedAt, surfaces: { ...core.surfaces, ...site.surfaces } };
}

function read(file) {
  return JSON.parse(readFileSync(file, "utf8"));
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  const [command, ...args] = process.argv.slice(2);
  if (command === "site-owner" && args.length === 3) {
    const [version, operation, manifestPath] = args;
    console.log(ownsPublicSite(version, operation, read(manifestPath)));
  } else if (command === "core" && args.length === 6) {
    const [version, sourceSHA, operation, cliPath, desktopPath, output] = args;
    writeFileSync(output, `${JSON.stringify(createCoreLedger({ version, sourceSHA, operation, cliRelease: read(cliPath), desktopRelease: read(desktopPath) }), null, 2)}\n`);
  } else if (command === "site" && args.length === 5) {
    const [version, sourceSHA, operation, manifestPath, output] = args;
    writeFileSync(output, `${JSON.stringify(createSiteLedger({ version, sourceSHA, operation, manifest: read(manifestPath) }), null, 2)}\n`);
  } else if (command === "merge" && args.length === 3) {
    const [corePath, sitePath, output] = args;
    writeFileSync(output, `${JSON.stringify(mergeLedgers(read(corePath), read(sitePath)), null, 2)}\n`);
  } else {
    throw new Error("usage: release-publication-ledger.mjs core VERSION SHA OPERATION CLI DESKTOP OUTPUT | site VERSION SHA OPERATION MANIFEST OUTPUT | merge CORE SITE OUTPUT");
  }
}
