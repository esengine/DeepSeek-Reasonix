import { createHash } from "node:crypto";
import { copyFileSync, lstatSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

export const platforms = ["darwin-universal", "windows-amd64", "windows-arm64", "linux-amd64"];
const fields = {
  sourceSHA: "RELEASE_SOURCE_SHA", controlSHA: "RELEASE_CONTROL_SHA",
  tag: "RELEASE_TAG", version: "RELEASE_VERSION", channel: "RELEASE_CHANNEL",
  signingFingerprint: "RELEASE_SIGNING_FINGERPRINT", prefix: "RELEASE_ARTIFACT_PREFIX",
};

export function releaseIdentity(env) {
  const identity = Object.fromEntries(Object.entries(fields).map(([key, variable]) => {
    if (!env[variable]) throw new Error(`missing ${variable}`);
    return [key, env[variable]];
  }));
  const match = /^desktop-([1-9][0-9]*)-([1-9][0-9]*)-(preflight|release)$/.exec(identity.prefix);
  if (!match || match[1] !== env.GITHUB_RUN_ID || !/^[1-9][0-9]*$/.test(env.GITHUB_RUN_ATTEMPT ?? "")
    || BigInt(match[2]) > BigInt(env.GITHUB_RUN_ATTEMPT)) throw new Error("artifact set is not from this run or a completed attempt");
  if (![identity.sourceSHA, identity.controlSHA].every(sha => /^[a-f0-9]{40}$/.test(sha))) throw new Error("invalid release SHA");
  return identity;
}

function entries(directory) {
  return readdirSync(directory).sort().map(name => {
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(name)) throw new Error(`unsafe artifact name: ${name}`);
    const file = path.join(directory, name);
    const stat = lstatSync(file);
    if (!stat.isFile() || stat.size === 0) throw new Error(`invalid artifact: ${name}`);
    return { name, size: stat.size, sha256: createHash("sha256").update(readFileSync(file)).digest("hex") };
  });
}

function requireSignatures(files) {
  const names = new Set(files.map(file => file.name));
  if (!names.size || !files.some(file => !file.name.endsWith(".minisig"))) throw new Error("empty artifact set");
  for (const name of names) {
    const companion = name.endsWith(".minisig") ? name.slice(0, -8) : `${name}.minisig`;
    if (!names.has(companion)) throw new Error(`missing payload or signature for ${name}`);
  }
}

function validAttempt(attempt, identity, currentAttempt) {
  return /^[1-9][0-9]*$/.test(attempt ?? "") && /^[1-9][0-9]*$/.test(currentAttempt ?? "")
    && BigInt(attempt) >= BigInt(identity.prefix.split("-")[2]) && BigInt(attempt) <= BigInt(currentAttempt);
}

export function pack(source, target, platform, identity, buildAttempt = identity.prefix.split("-")[2]) {
  if (!platforms.includes(platform)) throw new Error("invalid release platform");
  if (!validAttempt(buildAttempt, identity, buildAttempt)) throw new Error("invalid producer attempt");
  const files = entries(source);
  requireSignatures(files);
  mkdirSync(target); // Never append to a stale bundle.
  mkdirSync(path.join(target, "files"));
  for (const { name } of files) copyFileSync(path.join(source, name), path.join(target, "files", name));
  writeFileSync(path.join(target, "identity.json"), JSON.stringify({ schema: 1, ...identity, buildAttempt, platform, files }, null, 2));
}

export function collect(source, target, identity, currentAttempt = identity.prefix.split("-")[2]) {
  const expected = platforms.map(platform => `${identity.prefix}-${platform}`).sort();
  if (JSON.stringify(readdirSync(source).sort()) !== JSON.stringify(expected)) throw new Error("missing or unexpected platform bundle");
  const copies = new Map();
  for (const platform of platforms) {
    const bundle = path.join(source, `${identity.prefix}-${platform}`);
    if (!lstatSync(bundle).isDirectory() || lstatSync(bundle).isSymbolicLink()
      || JSON.stringify(readdirSync(bundle).sort()) !== JSON.stringify(["files", "identity.json"])) throw new Error("invalid bundle layout");
    if (!lstatSync(path.join(bundle, "identity.json")).isFile() || lstatSync(path.join(bundle, "identity.json")).isSymbolicLink()
      || !lstatSync(path.join(bundle, "files")).isDirectory() || lstatSync(path.join(bundle, "files")).isSymbolicLink()) throw new Error("invalid bundle entries");
    const manifest = JSON.parse(readFileSync(path.join(bundle, "identity.json"), "utf8"));
    if (manifest.schema !== 1 || manifest.platform !== platform) throw new Error("invalid bundle identity");
    if (!validAttempt(manifest.buildAttempt, identity, currentAttempt)) throw new Error("invalid producer attempt");
    for (const [key, value] of Object.entries(identity)) {
      if (manifest[key] !== value) throw new Error(`artifact identity mismatch: ${key}`);
    }
    const files = entries(path.join(bundle, "files"));
    if (JSON.stringify(files) !== JSON.stringify(manifest.files)) throw new Error(`artifact digest mismatch: ${platform}`);
    requireSignatures(files);
    for (const { name } of files) {
      if (copies.has(name)) throw new Error(`duplicate artifact across platforms: ${name}`);
      copies.set(name, path.join(bundle, "files", name));
    }
  }
  // Complete identity and digest verification precedes any publication input.
  mkdirSync(target);
  for (const [name, file] of copies) copyFileSync(file, path.join(target, name));
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  const [command, source, target, platform] = process.argv.slice(2);
  const identity = releaseIdentity(process.env);
  if (command === "pack") pack(source, target, platform, identity, process.env.GITHUB_RUN_ATTEMPT);
  else if (command === "collect") collect(source, target, identity, process.env.GITHUB_RUN_ATTEMPT);
  else throw new Error("usage: desktop-release-artifacts.mjs pack|collect SOURCE TARGET [PLATFORM]");
}
