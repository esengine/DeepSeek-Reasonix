#!/usr/bin/env node
import { readFileSync } from "node:fs";

const path = process.argv[2];
if (!path) {
  console.error("usage: analyze-cpu-prof.mjs <file.cpuprofile>");
  process.exit(2);
}

const prof = JSON.parse(readFileSync(path, "utf8"));
const nodes = prof.nodes;
const samples = prof.samples;
const deltas = prof.timeDeltas;

const byId = new Map();
for (const n of nodes) byId.set(n.id, n);

const selfMicros = new Map();
for (let i = 0; i < samples.length; i++) {
  const id = samples[i];
  const dt = deltas[i];
  selfMicros.set(id, (selfMicros.get(id) ?? 0) + dt);
}

const totalMicros = new Map();
const childOf = new Map();
for (const n of nodes) {
  for (const c of n.children ?? []) {
    childOf.set(c, n.id);
  }
}

function ancestors(id) {
  const out = [];
  let cur = id;
  while (cur !== undefined) {
    out.push(cur);
    cur = childOf.get(cur);
  }
  return out;
}

for (const [leaf, micros] of selfMicros) {
  for (const a of ancestors(leaf)) {
    totalMicros.set(a, (totalMicros.get(a) ?? 0) + micros);
  }
}

const totalRun = [...deltas].reduce((a, b) => a + b, 0);

const IDLE_FNS = new Set(["(idle)", "(program)", "(garbage collector)"]);
let activeMicros = 0;
for (const [id, micros] of selfMicros) {
  const n = byId.get(id);
  if (!IDLE_FNS.has(n.callFrame.functionName)) activeMicros += micros;
}

const rows = [...totalMicros.entries()]
  .map(([id, total]) => {
    const n = byId.get(id);
    const cf = n.callFrame;
    const file = (cf.url || "").replace(/^file:\/\/\//, "").replace(/.*[\\/]/, "");
    const fn = cf.functionName || "(anonymous)";
    const self = selfMicros.get(id) ?? 0;
    return {
      id,
      fn,
      file: file ? `${file}:${cf.lineNumber + 1}` : "(native)",
      url: cf.url || "",
      total,
      self,
    };
  })
  .filter(
    (r) =>
      !/node:internal|node_modules[\\/]tsx|node_modules[\\/]esbuild/.test(r.url) &&
      !IDLE_FNS.has(r.fn),
  )
  .sort((a, b) => b.total - a.total);

const fmt = (us) =>
  `${(us / 1000).toFixed(1).padStart(7)} ms (${((us / Math.max(1, activeMicros)) * 100).toFixed(1).padStart(4)}% active / ${((us / totalRun) * 100).toFixed(1).padStart(4)}% wall)`;

console.log(
  `Total wall: ${(totalRun / 1000).toFixed(0)} ms across ${samples.length} samples · active CPU: ${(activeMicros / 1000).toFixed(0)} ms · idle: ${((totalRun - activeMicros) / 1000).toFixed(0)} ms\n`,
);

console.log("Top 30 by inclusive time (excludes node-internal):");
console.log("─".repeat(120));
for (const r of rows.slice(0, 30)) {
  console.log(`  ${fmt(r.total)} self=${fmt(r.self)}  ${r.fn.padEnd(40)} @ ${r.file}`);
}

console.log("\nTop 20 by self time:");
console.log("─".repeat(120));
const bySelf = [...selfMicros.entries()]
  .map(([id, self]) => {
    const n = byId.get(id);
    const cf = n.callFrame;
    const file = (cf.url || "").replace(/^file:\/\/\//, "").replace(/.*[\\/]/, "");
    return {
      fn: cf.functionName || "(anonymous)",
      file: file ? `${file}:${cf.lineNumber + 1}` : "(native)",
      url: cf.url || "",
      self,
    };
  })
  .filter(
    (r) =>
      !IDLE_FNS.has(r.fn) &&
      !/node:internal|node_modules[\\/]tsx|node_modules[\\/]esbuild/.test(r.url),
  )
  .sort((a, b) => b.self - a.self);
for (const r of bySelf.slice(0, 20)) {
  console.log(`  ${fmt(r.self)}  ${r.fn.padEnd(40)} @ ${r.file}`);
}
