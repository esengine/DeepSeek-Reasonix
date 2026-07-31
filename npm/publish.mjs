import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";

const CANDIDATE_SHA_RE = /^[0-9a-f]{40}$/;
const STABLE_RE = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/;
const CANARY_RE = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-canary\.(0|[1-9][0-9]*)$/;
const SEMVER_RE = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/;

function compareNumeric(a, b) {
  const normalizedA = a.replace(/^0+(?=\d)/, "");
  const normalizedB = b.replace(/^0+(?=\d)/, "");
  if (normalizedA.length !== normalizedB.length) {
    return normalizedA.length > normalizedB.length ? 1 : -1;
  }
  if (normalizedA === normalizedB) return 0;
  return normalizedA > normalizedB ? 1 : -1;
}

function compareIdentifiers(a, b) {
  const numericA = /^[0-9]+$/.test(a);
  const numericB = /^[0-9]+$/.test(b);
  if (numericA && numericB) return compareNumeric(a, b);
  if (numericA !== numericB) return numericA ? -1 : 1;
  if (a === b) return 0;
  return a > b ? 1 : -1;
}

function parseSemver(version) {
  const match = String(version).match(SEMVER_RE);
  if (!match) throw new Error(`invalid npm release version: ${version}`);
  return {
    core: match.slice(1, 4),
    prerelease: match[4] ? match[4].split(".") : [],
  };
}

function compareSemver(a, b) {
  const aa = parseSemver(a);
  const bb = parseSemver(b);
  for (let i = 0; i < aa.core.length; i += 1) {
    const compared = compareNumeric(aa.core[i], bb.core[i]);
    if (compared !== 0) return compared;
  }
  if (!aa.prerelease.length || !bb.prerelease.length) {
    if (aa.prerelease.length === bb.prerelease.length) return 0;
    return aa.prerelease.length ? -1 : 1;
  }
  const count = Math.max(aa.prerelease.length, bb.prerelease.length);
  for (let i = 0; i < count; i += 1) {
    if (aa.prerelease[i] === undefined) return -1;
    if (bb.prerelease[i] === undefined) return 1;
    const compared = compareIdentifiers(aa.prerelease[i], bb.prerelease[i]);
    if (compared !== 0) return compared;
  }
  return 0;
}

function requireVersionForDistTag(distTag, version) {
  if (distTag === "latest" && STABLE_RE.test(version)) return;
  if (distTag === "canary" && CANARY_RE.test(version)) return;
  if (
    distTag === "next" &&
    SEMVER_RE.test(version) &&
    version.includes("-") &&
    !CANARY_RE.test(version)
  ) {
    return;
  }
  throw new Error(`version ${version} does not belong to npm dist-tag ${distTag}`);
}

export function distTagForVersion(version) {
  if (CANARY_RE.test(version)) return "canary";
  if (SEMVER_RE.test(version) && version.includes("-")) return "next";
  if (STABLE_RE.test(version)) return "latest";
  throw new Error(`invalid npm release version: ${version}`);
}

// Returns a positive value when candidate is newer, zero when it is current,
// and a negative value when recovery is for an older immutable version.
export function compareDistTagVersions(distTag, candidate, current) {
  requireVersionForDistTag(distTag, candidate);
  if (!current) return 1;
  requireVersionForDistTag(distTag, current);
  return compareSemver(candidate, current);
}

function defaultSleep(milliseconds) {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, milliseconds);
}

function defaultRunner(args, { cwd, missingOk = false, inherit = false } = {}) {
  const result = spawnSync("npm", args, {
    cwd,
    encoding: "utf8",
    env: process.env,
    stdio: inherit ? "inherit" : ["ignore", "pipe", "pipe"],
  });
  if (result.status === 0) return inherit ? "" : result.stdout.trim();

  const output = `${result.stdout || ""}\n${result.stderr || ""}`;
  if (missingOk && /\bE404\b/.test(output)) return null;
  const detail = output.trim();
  throw new Error(
    `npm ${args[0]} failed with exit code ${result.status}${detail ? `: ${detail}` : ""}`,
  );
}

function parseJSON(output, description) {
  if (output === null || output === "") return null;
  try {
    return JSON.parse(output);
  } catch (error) {
    throw new Error(`${description} returned invalid JSON: ${error.message}`);
  }
}

function readLocalPackage(entry, version, candidateSha) {
  const pkg = JSON.parse(readFileSync(`${entry.dir}/package.json`, "utf8"));
  if (pkg.name !== entry.name || pkg.version !== version) {
    throw new Error(
      `local package identity mismatch: expected ${entry.name}@${version}, got ${pkg.name}@${pkg.version}`,
    );
  }
  if (pkg.reasonixCandidateSha !== candidateSha) {
    throw new Error(
      `${entry.name}@${version} does not record candidate ${candidateSha}`,
    );
  }
  return pkg;
}

function registryPackage(runner, name, version) {
  const output = runner(
    [
      "view",
      `${name}@${version}`,
      "name",
      "version",
      "reasonixCandidateSha",
      "gitHead",
      "--json",
    ],
    { missingOk: true },
  );
  return parseJSON(output, `${name}@${version} metadata`);
}

function verifyRegistryPackage(metadata, name, version, candidateSha) {
  if (!metadata) return false;
  if (metadata.name !== name || metadata.version !== version) {
    throw new Error(
      `registry package identity mismatch: expected ${name}@${version}, got ${metadata.name}@${metadata.version}`,
    );
  }

  const recordedCandidate = metadata.reasonixCandidateSha;
  const gitHead = metadata.gitHead;
  if (recordedCandidate && recordedCandidate !== candidateSha) {
    throw new Error(
      `immutable npm package ${name}@${version} belongs to candidate ${recordedCandidate}, expected ${candidateSha}`,
    );
  }
  if (gitHead && gitHead !== candidateSha) {
    throw new Error(
      `immutable npm package ${name}@${version} has gitHead ${gitHead}, expected ${candidateSha}`,
    );
  }
  if (!recordedCandidate && !gitHead) {
    throw new Error(
      `immutable npm package ${name}@${version} has no candidate provenance`,
    );
  }
  return true;
}

function readDistTag(runner, name, distTag) {
  const output = runner(
    ["view", name, `dist-tags.${distTag}`, "--json"],
    { missingOk: true },
  );
  const value = parseJSON(output, `${name} dist-tag ${distTag}`);
  if (value === null || value === undefined) return "";
  if (typeof value !== "string") {
    throw new Error(`${name} dist-tag ${distTag} returned a non-string value`);
  }
  return value;
}

function waitForPackage(
  runner,
  entry,
  version,
  candidateSha,
  attempts,
  sleep,
) {
  for (let attempt = 1; attempt <= attempts; attempt += 1) {
    const metadata = registryPackage(runner, entry.name, version);
    if (metadata) {
      verifyRegistryPackage(metadata, entry.name, version, candidateSha);
      return;
    }
    if (attempt < attempts) sleep(10_000);
  }
  throw new Error(`${entry.name}@${version} did not become visible in the npm registry`);
}

function ensurePackage(
  runner,
  entry,
  version,
  candidateSha,
  stagingTag,
  attempts,
  sleep,
  log,
) {
  readLocalPackage(entry, version, candidateSha);
  const existing = registryPackage(runner, entry.name, version);
  if (existing) {
    verifyRegistryPackage(existing, entry.name, version, candidateSha);
    log(`reuse ${entry.name}@${version} from candidate ${candidateSha}`);
    return;
  }

  log(`publish ${entry.name}@${version} (${stagingTag})`);
  try {
    runner(
      ["publish", "--access", "public", "--tag", stagingTag],
      { cwd: entry.dir, inherit: true },
    );
  } catch (error) {
    // A concurrent or retried publisher may have won after our read. Accept it
    // only when the immutable registry metadata proves the same candidate.
    const raced = registryPackage(runner, entry.name, version);
    if (!raced) throw error;
    verifyRegistryPackage(raced, entry.name, version, candidateSha);
  }
  waitForPackage(
    runner,
    entry,
    version,
    candidateSha,
    attempts,
    sleep,
  );
}

function advanceDistTag(runner, name, version, distTag, attempts, sleep, log) {
  const current = readDistTag(runner, name, distTag);
  const comparison = compareDistTagVersions(distTag, version, current);
  if (comparison > 0) {
    log(`advance ${name} ${distTag}: ${current || "<unset>"} -> ${version}`);
    runner(["dist-tag", "add", `${name}@${version}`, distTag], { inherit: true });
  } else if (comparison === 0) {
    log(`${name} ${distTag} already points to ${version}`);
  } else {
    log(`keep newer ${name} ${distTag} at ${current}; recovered ${version}`);
  }

  for (let attempt = 1; attempt <= attempts; attempt += 1) {
    const observed = readDistTag(runner, name, distTag);
    if (
      observed &&
      compareDistTagVersions(distTag, observed, version) >= 0
    ) {
      return;
    }
    if (attempt < attempts) sleep(10_000);
  }
  throw new Error(`${name} dist-tag ${distTag} did not reach ${version} or newer`);
}

function cleanupStagingTag(runner, name, version, stagingTag, log) {
  const current = readDistTag(runner, name, stagingTag);
  if (current !== version) return;
  log(`remove temporary ${name} dist-tag ${stagingTag}`);
  runner(["dist-tag", "rm", name, stagingTag], { inherit: true });
}

export function publishPackages({
  packages,
  version,
  candidateSha,
  runner = defaultRunner,
  sleep = defaultSleep,
  attempts = 6,
  log = console.log,
}) {
  if (!Array.isArray(packages) || packages.length === 0) {
    throw new Error("npm publication requires at least one package");
  }
  if (!CANDIDATE_SHA_RE.test(candidateSha)) {
    throw new Error(`invalid release candidate SHA: ${candidateSha}`);
  }
  const distTag = distTagForVersion(version);
  const stagingTag = `${distTag}-staging`;
  let failure;

  try {
    for (const entry of packages) {
      ensurePackage(
        runner,
        entry,
        version,
        candidateSha,
        stagingTag,
        attempts,
        sleep,
        log,
      );
    }
    for (const entry of packages) {
      advanceDistTag(
        runner,
        entry.name,
        version,
        distTag,
        attempts,
        sleep,
        log,
      );
    }
  } catch (error) {
    failure = error;
  }

  try {
    for (const entry of packages) {
      cleanupStagingTag(runner, entry.name, version, stagingTag, log);
    }
  } catch (error) {
    if (!failure) failure = error;
  }

  if (failure) throw failure;
  return { distTag, version };
}
