#!/usr/bin/env node

import { readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { loadCatalog, releaseForVersion } from "./release-notes.mjs";

function invariant(condition, message) {
  if (!condition) throw new Error(message);
}

function parseArgs(argv) {
  const [command = "validate", ...rest] = argv;
  const values = { command };
  for (let index = 0; index < rest.length; index += 1) {
    const arg = rest[index];
    if (!arg.startsWith("--")) throw new Error(`unexpected argument ${arg}`);
    values[arg.slice(2)] = rest[++index];
  }
  return values;
}

export function validateReleaseEvent(event, release) {
  invariant(event && typeof event === "object" && !Array.isArray(event), "release event must be an object");
  invariant(event.schemaVersion === 1, "release event schemaVersion must be 1");
  invariant(event.releaseId === release.version, "release event releaseId does not match reviewed notes");
  invariant(
    event.channel === (release.channel === "prerelease" ? "preview" : "stable"),
    "release event channel does not match reviewed notes",
  );
  invariant(/^[0-9a-f]{40}$/.test(event.candidateSha), "release event candidateSha must be a full commit SHA");
  if (release.candidateSha) {
    invariant(event.candidateSha === release.candidateSha, "release event candidateSha does not match reviewed notes");
  }
  invariant(/^\d{4}-\d{2}-\d{2}T/.test(event.publishedAt), "release event publishedAt must be an ISO timestamp");
  invariant(event.releaseNotesUrl === `https://reasonix.io/changelog/v${release.version}/`, "release event URL is invalid");
  invariant(event.builds && typeof event.builds === "object", "release event builds are required");
  for (const surface of ["cli", "desktop", "npm"]) {
    invariant(typeof event.builds[surface] === "string" && event.builds[surface], `release event builds.${surface} is required`);
    if (release.builds?.[surface]) {
      invariant(event.builds[surface] === release.builds[surface], `release event builds.${surface} does not match reviewed notes`);
    }
  }
  return event;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  invariant(args.version, "--version is required");
  const catalog = await loadCatalog(args.catalog ? resolve(args.catalog) : undefined);
  const release = releaseForVersion(catalog, args.version);

  if (args.command === "generate") {
    invariant(args.sha, "generate requires --sha");
    invariant(args.output, "generate requires --output");
    const event = validateReleaseEvent({
      schemaVersion: 1,
      releaseId: release.version,
      channel: release.channel === "prerelease" ? "preview" : "stable",
      candidateSha: args.sha,
      publishedAt: args["published-at"] || new Date().toISOString(),
      releaseNotesUrl: `https://reasonix.io/changelog/v${release.version}/`,
      builds: release.builds,
    }, release);
    await writeFile(resolve(args.output), `${JSON.stringify(event, null, 2)}\n`);
    return;
  }

  invariant(args.command === "validate", `unknown command ${args.command}`);
  invariant(args.input, "validate requires --input");
  const event = JSON.parse(await readFile(resolve(args.input), "utf8"));
  validateReleaseEvent(event, release);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
