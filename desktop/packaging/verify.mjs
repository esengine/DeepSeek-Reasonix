#!/usr/bin/env node
// Lists a desktop artifact (bundle directory, .zip, .tar.gz or .deb) and fails
// unless every member the install layout relies on is present.
//
// usage: node desktop/packaging/verify.mjs <artifact> [--kind <kind>] [--list]
import { spawnSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { resolve } from "node:path";
import { ARTIFACT_KINDS, checkMembers, inferArtifactKind, isDirectory, listZipEntries, walkFiles } from "./lib.mjs";

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

function entriesOf(path) {
  if (isDirectory(path)) return walkFiles(path);
  if (path.endsWith(".zip")) return listZipEntries(path);
  if (path.endsWith(".tar.gz")) return tool("tar", ["-tzf", path]);
  if (path.endsWith(".deb")) return tool("dpkg-deb", ["-c", path]).map((line) => line.split(/\s+/)[5] ?? "");
  throw new Error(`unsupported artifact ${path}`);
}

const directory = isDirectory(artifact);
const kind = kindArg ?? inferArtifactKind(artifact, directory, directory ? readdirSync(artifact) : []);
const entries = entriesOf(artifact);
if (args.includes("--list")) for (const entry of entries) console.log(entry);
const { missing, forbidden } = checkMembers(entries, kind);
for (const name of missing) console.error(`verify: ${kind} is missing ${name}`);
for (const name of forbidden) console.error(`verify: ${kind} must not contain ${name}`);
if (missing.length > 0 || forbidden.length > 0) process.exit(1);
console.log(`verify: ${kind} ok (${entries.length} entries in ${artifact})`);
