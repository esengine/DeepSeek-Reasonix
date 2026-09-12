#!/usr/bin/env node
// Lists a desktop artifact (bundle directory, .zip, .tar.gz or .deb) and fails
// unless every member the install layout relies on is present. Linux archives
// also fail when a directory or file would be unreadable to other users.
//
// usage: node desktop/packaging/verify.mjs <artifact> [--kind <kind>] [--list]
import { spawnSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { resolve } from "node:path";
import { ARTIFACT_KINDS, checkEntryModes, checkMembers, inferArtifactKind, isDirectory, listZipEntries, parseVerboseListing, validateMacServiceLink, walkFiles } from "./lib.mjs";

const args = process.argv.slice(2);
const artifactArg = args.find((arg) => !arg.startsWith("--"));
const kindIndex = args.indexOf("--kind");
const kindArg = kindIndex >= 0 ? args[kindIndex + 1] : undefined;
if (!artifactArg || (kindArg && !ARTIFACT_KINDS.includes(kindArg))) {
  console.error(`usage: verify.mjs <artifact> [--kind <${ARTIFACT_KINDS.join("|")}>] [--list]`);
  process.exit(2);
}
const artifact = resolve(artifactArg);

function tool(command, toolArgs) {
  const result = spawnSync(command, toolArgs, { encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
  if (result.status !== 0) throw new Error(`${command} ${toolArgs.join(" ")} failed: ${result.stderr || result.error?.message || result.status}`);
  return result.stdout.split(/\r?\n/).filter((line) => line !== "");
}

function listingOf(path) {
  if (isDirectory(path)) return { entries: walkFiles(path), rows: [] };
  if (path.endsWith(".zip")) return { entries: listZipEntries(path), rows: [] };
  let rows;
  if (path.endsWith(".tar.gz")) rows = parseVerboseListing(tool("tar", ["-tvzf", path]));
  else if (path.endsWith(".deb")) rows = parseVerboseListing(tool("dpkg-deb", ["-c", path]));
  else throw new Error(`unsupported artifact ${path}`);
  return { entries: rows.map((row) => row.name), rows };
}

const directory = isDirectory(artifact);
const kind = kindArg ?? inferArtifactKind(artifact, directory, directory ? readdirSync(artifact) : []);
const { entries, rows } = listingOf(artifact);
if (args.includes("--list")) for (const entry of entries) console.log(entry);
const { missing, forbidden } = checkMembers(entries, kind);
const layoutErrors = kind === "darwin-app-dir" ? validateMacServiceLink(artifact) : [];
const modeErrors = checkEntryModes(rows, kind);
for (const name of missing) console.error(`verify: ${kind} is missing ${name}`);
for (const name of forbidden) console.error(`verify: ${kind} must not contain ${name}`);
for (const error of layoutErrors) console.error(`verify: ${kind} ${error}`);
for (const error of modeErrors) console.error(`verify: ${kind} ${error}`);
if (missing.length > 0 || forbidden.length > 0 || layoutErrors.length > 0 || modeErrors.length > 0) process.exit(1);
console.log(`verify: ${kind} ok (${entries.length} entries in ${artifact})`);
