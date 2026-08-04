#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { setTimeout as delay } from "node:timers/promises";

const version = process.argv[2];
const expectedSha = process.env.EXPECTED_SHA || "";
const verifyDelayMs = Number.parseInt(process.env.NPM_TAG_VERIFY_DELAY_MS || "2000", 10);
const verifyAttempts = Number.parseInt(process.env.NPM_TAG_VERIFY_ATTEMPTS || "15", 10);
if (!/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(version || "")) {
  throw new Error("usage: finalize-npm-official-release.mjs MAJOR.MINOR.PATCH");
}
if (expectedSha && !/^[0-9a-f]{40}$/.test(expectedSha)) {
  throw new Error("EXPECTED_SHA must be a full lowercase commit SHA");
}
if (!Number.isSafeInteger(verifyDelayMs) || verifyDelayMs < 0) {
  throw new Error("NPM_TAG_VERIFY_DELAY_MS must be a non-negative integer");
}
if (!Number.isSafeInteger(verifyAttempts) || verifyAttempts < 1) {
  throw new Error("NPM_TAG_VERIFY_ATTEMPTS must be a positive integer");
}

const packages = [
  "reasonix",
  "@reasonix/cli-darwin-arm64", "@reasonix/cli-darwin-x64",
  "@reasonix/cli-linux-arm64", "@reasonix/cli-linux-x64",
  "@reasonix/cli-win32-arm64", "@reasonix/cli-win32-x64",
];

const OFFICIAL_ALIASES = ["latest", "canary", "next"];

// The publisher stages a stable release under "<dist-tag>-staging" and removes
// it once the official aliases land (npm/publish.mjs). This cleanup used to name
// "official-staging", which nothing creates, so it never removed anything and a
// staging alias left behind by a failed publish stayed on the registry.
const stagingTag = "latest-staging";

function metadata(name) {
  const output = execFileSync(
    "npm",
    ["view", `${name}@${version}`, "name", "version", "gitHead", "reasonixCandidateSha", "--json"],
    { encoding: "utf8" },
  );
  return JSON.parse(output);
}

function distTags(name) {
  return JSON.parse(execFileSync("npm", ["view", name, "dist-tags", "--json"], { encoding: "utf8" }));
}

async function waitForOfficialAliases(name) {
  let tags = {};
  for (let attempt = 1; attempt <= verifyAttempts; attempt += 1) {
    tags = distTags(name);
    if (OFFICIAL_ALIASES.every((tag) => tags[tag] === version)) return tags;
    if (attempt < verifyAttempts) await delay(verifyDelayMs);
  }
  const actual = OFFICIAL_ALIASES
    .map((tag) => `${tag}=${tags[tag] || "<missing>"}`)
    .join(", ");
  throw new Error(`${name} aliases did not converge to ${version}: ${actual}`);
}

for (const name of packages) {
  const actual = metadata(name);
  if (actual.name !== name || actual.version !== version) {
    throw new Error(`${name}@${version} is unavailable or resolved to another package`);
  }
  if (expectedSha && actual.gitHead !== expectedSha) {
    throw new Error(`${name}@${version} gitHead ${actual.gitHead || "<missing>"} does not match ${expectedSha}`);
  }
  if (expectedSha && actual.reasonixCandidateSha !== expectedSha) {
    throw new Error(`${name}@${version} reasonixCandidateSha ${actual.reasonixCandidateSha || "<missing>"} does not match ${expectedSha}`);
  }
}

// Recovery reruns this after an alias write already succeeded — including after
// someone realigned the aliases by hand because the automation could not. Every
// dist-tag write is a registry mutation that can be refused (npm treats them as
// sensitive operations, so an automation token gets a 403 where an interactive
// session does not), and refusing a write we did not need would fail a rerun
// that had nothing left to do. So read first and only write what is missing.
for (const name of packages) {
  const before = distTags(name);
  for (const tag of OFFICIAL_ALIASES) {
    if (before[tag] === version) continue;
    execFileSync("npm", ["dist-tag", "add", `${name}@${version}`, tag], { stdio: "inherit" });
  }
  const tags = await waitForOfficialAliases(name);
  if (tags[stagingTag] === version) {
    execFileSync("npm", ["dist-tag", "rm", name, stagingTag], { stdio: "inherit" });
  }
}
