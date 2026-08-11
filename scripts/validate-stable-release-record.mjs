#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { isDeepStrictEqual } from "node:util";
import { releaseForVersion, validateCatalog } from "./release-notes.mjs";

const [catalogPath, version, previousCatalogPath] = process.argv.slice(2);
if (!catalogPath || !/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(version || "")) {
  throw new Error("usage: validate-stable-release-record.mjs CATALOG MAJOR.MINOR.PATCH [PREVIOUS_CATALOG]");
}

const catalog = validateCatalog(JSON.parse(readFileSync(catalogPath, "utf8")));
const release = releaseForVersion(catalog, version);
if (release.version !== version || release.channel !== "stable" || release.status !== "reviewed") {
  throw new Error(`v${version} must have one reviewed Stable release record`);
}

if (previousCatalogPath) {
  const previousCatalog = validateCatalog(JSON.parse(readFileSync(previousCatalogPath, "utf8")));
  const previousRelease = previousCatalog.releases.find((entry) => entry.version === version);
  if (previousRelease && isDeepStrictEqual(previousRelease, release)) {
    throw new Error(`v${version} reviewed Stable record was not introduced or changed by the candidate commit`);
  }
}

console.log(`reviewed Stable release record verified: v${version}`);
