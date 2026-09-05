import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";

export function buildIdentity(frontendDir) {
  const hash = createHash("sha256");
  function visit(directory) {
    for (const entry of readdirSync(directory, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      const file = path.join(directory, entry.name);
      if (entry.isDirectory()) visit(file);
      else hash.update(path.relative(frontendDir, file)).update(readFileSync(file));
    }
  }
  visit(path.join(frontendDir, "dist"));
  const git = (...args) => execFileSync("git", args, { cwd: frontendDir, encoding: "utf8" }).trim();
  const untracked = execFileSync("git", ["ls-files", "--others", "--exclude-standard", "-z", "--", "."], { cwd: frontendDir, encoding: "utf8" }).split("\0").filter(Boolean).sort();
  const untrackedHash = createHash("sha256");
  for (const file of untracked) untrackedHash.update(file).update("\0").update(readFileSync(path.join(frontendDir, file))).update("\0");
  return {
    sourceSHA: git("rev-parse", "HEAD"),
    trackedDiffSHA256: createHash("sha256").update(git("diff", "HEAD", "--", ".")).digest("hex"),
    untrackedSourceSHA256: untrackedHash.digest("hex"),
    sourceStatus: git("status", "--porcelain", "--", "."),
    buildSHA256: hash.digest("hex"),
    node: process.version, platform: process.platform, arch: process.arch,
  };
}

// IDs, not count deltas: an increasing population can hide behind simultaneous GC.
export function retainedCohorts(samples) {
  const firstSeen = new Map();
  return samples.map((sample, index) => {
    const ids = sample.lifecycle.liveRenderTokenIds;
    for (const id of ids) if (!firstSeen.has(id)) firstSeen.set(id, index);
    return {
      phase: sample.phase, roundTrips: sample.roundTrips,
      survivorsFromBaseline: ids.filter(id => firstSeen.get(id) === 0),
      // Two later observations after completed round trips; not a whole-App count budget.
      retainedPostBaseline: ids.filter(id => firstSeen.get(id) > 0 && firstSeen.get(id) < index - 1),
    };
  });
}

export function evidenceIntegrity(samples) {
  return samples.length > 0 && samples.every(({ lifecycle }) => (
    Array.isArray(lifecycle.liveRenderTokenIds)
    && new Set(lifecycle.liveRenderTokenIds).size === lifecycle.liveRenderTokenIds.length
    && lifecycle.liveRenderTokenIds.length === lifecycle.liveRenderTokens
    && lifecycle.overflow === false && lifecycle.invariantViolations === 0
    && lifecycle.activeOperations >= 0 && lifecycle.activeSubscriptions >= 0
  ));
}

// CDP's DOM counter includes attached and detached nodes. Only the heap's
// detachedness field can label an object as detached; unknown stays unknown.
export function summarizeHeap(snapshot) {
  const { node_fields: fields, node_types: types } = snapshot.snapshot.meta;
  const width = fields.length;
  const at = Object.fromEntries(fields.map((field, index) => [field, index]));
  const categories = {};
  const detached = {};
  for (let offset = 0; offset < snapshot.nodes.length; offset += width) {
    const type = types[at.type][snapshot.nodes[offset + at.type]];
    const category = categories[type] ??= { count: 0, selfBytes: 0 };
    category.count++;
    category.selfBytes += snapshot.nodes[offset + at.self_size];
    if (at.detachedness !== undefined && snapshot.nodes[offset + at.detachedness] === 2) {
      const name = snapshot.strings[snapshot.nodes[offset + at.name]];
      const entry = detached[name] ??= { count: 0, selfBytes: 0, ids: [] };
      entry.count++;
      entry.selfBytes += snapshot.nodes[offset + at.self_size];
      entry.ids.push(snapshot.nodes[offset + at.id]);
    }
  }
  return { categories, detached, detachednessAvailable: at.detachedness !== undefined };
}
