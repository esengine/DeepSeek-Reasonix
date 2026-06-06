#!/usr/bin/env node
// Roll up a .cpuprofile into a flat self-time + total-time table by function.
// Usage: node scripts/analyze-cpuprofile.mjs <path>.cpuprofile

import { readFileSync } from "node:fs";

const path = process.argv[2];
if (!path) {
  process.stderr.write("usage: analyze-cpuprofile.mjs <path>\n");
  process.exit(1);
}

const prof = JSON.parse(readFileSync(path, "utf8"));
const nodesById = new Map(prof.nodes.map((n) => [n.id, n]));
const parentById = new Map();
for (const n of prof.nodes) {
  for (const child of n.children ?? []) parentById.set(child, n.id);
}

// timeDeltas[i] = microseconds attributed to samples[i] (the LEAF frame at that tick).
const samples = prof.samples;
const deltas = prof.timeDeltas;
if (samples.length !== deltas.length) {
  process.stderr.write(
    `warn: samples=${samples.length} deltas=${deltas.length}\n`,
  );
}

const selfUs = new Map(); // nodeId -> microseconds (leaf only)
const totalUs = new Map(); // function key -> microseconds (any frame in stack)

const totalSampled = deltas.reduce((a, b) => a + b, 0);

for (let i = 0; i < samples.length; i++) {
  const leaf = samples[i];
  const dt = deltas[i] ?? 0;
  selfUs.set(leaf, (selfUs.get(leaf) ?? 0) + dt);
  // Walk up
  const seen = new Set();
  let cur = leaf;
  while (cur !== undefined) {
    const node = nodesById.get(cur);
    if (!node) break;
    const key = funcKey(node);
    if (!seen.has(key)) {
      totalUs.set(key, (totalUs.get(key) ?? 0) + dt);
      seen.add(key);
    }
    cur = parentById.get(cur);
  }
}

function funcKey(node) {
  const cf = node.callFrame;
  const url = (cf.url || "").replace(/^file:\/\/\//, "").replace(/^.*[\\/]/, "");
  return `${cf.functionName || "(anonymous)"} ${url}:${cf.lineNumber + 1}`;
}

// Collapse self-time by funcKey too
const selfByKey = new Map();
for (const [nid, us] of selfUs) {
  const node = nodesById.get(nid);
  if (!node) continue;
  const k = funcKey(node);
  selfByKey.set(k, (selfByKey.get(k) ?? 0) + us);
}

function fmt(us) {
  const ms = us / 1000;
  const pct = ((us / totalSampled) * 100).toFixed(1);
  return `${ms.toFixed(0).padStart(6)}ms ${pct.padStart(5)}%`;
}

process.stdout.write(
  `total profile: ${(totalSampled / 1000).toFixed(0)} ms\n\n`,
);

const topSelf = [...selfByKey.entries()].sort((a, b) => b[1] - a[1]).slice(0, 30);
process.stdout.write("=== TOP SELF (where CPU actually burns) ===\n");
for (const [k, us] of topSelf) {
  process.stdout.write(`  ${fmt(us)}  ${k}\n`);
}

process.stdout.write("\n=== TOP TOTAL (any frame on stack) ===\n");
const topTotal = [...totalUs.entries()].sort((a, b) => b[1] - a[1]).slice(0, 30);
for (const [k, us] of topTotal) {
  process.stdout.write(`  ${fmt(us)}  ${k}\n`);
}
