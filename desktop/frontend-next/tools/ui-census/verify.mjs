// Two jobs, kept apart because they answer different questions.
//
// Default: the fixture corpus's frozen reports. _fx is a tree written to
// exercise the rules, so its output moves only when the analyzer's judgement
// moves — which makes it a permanent gate. The product tree's reports name
// every root by file and line and are stale the moment a button moves, so they
// are never frozen here.
//
// --snapshot <dir> / --against <dir>: the migration workflow. Extraction is a
// change of where code lives, and nothing about moving a function may move a
// verdict, a witness or an open edge. Snapshot every report over both trees
// before, compare after, and the claim is checked rather than asserted.
import { spawnSync } from "node:child_process";
import { readdirSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join, dirname, isAbsolute } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..", "..");
const entry = process.env.CENSUS_ENTRY ?? join(here, "main.mjs");

const PROBES = ["effects", "verdicts", "oracle", "domroots", "apis", "state", "life", "cross",
  "r2", "roots", "origin", "actions", "unnamed", "candidates", "transport", "endpoints"];
const SHOWS = ["unresolved", "sinks", "unnamed"];
const FX_PROBES = ["state", "life", "cross", "r2", "roots", "effects", "transport", "endpoints", "b0"];
const FX = { CENSUS_SRC: "_fx", CENSUS_ROOTS: "_fx/main.tsx" };

const fixtureOutputs = new Map(FX_PROBES.map((p) => ["fx-" + p + ".txt", { PROBE: p, SHOW: "", ...FX }]));
const productOutputs = new Map([
  ...PROBES.map((p) => ["probe-" + p + ".txt", { PROBE: p, SHOW: "" }]),
  ...SHOWS.map((s) => ["show-" + s + ".txt", { PROBE: "", SHOW: s }]),
  ["default.txt", { PROBE: "", SHOW: "" }],
]);

// Both streams, the way the reports were captured: an invariant failure is
// written to stderr and belongs in the comparison.
const run = (env) => {
  const r = spawnSync("node", [entry], { cwd: root, env: { ...process.env, ...env },
    encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
  return (r.stdout ?? "") + (r.stderr ?? "");
};

const compare = (dir, outputs) => {
  const onDisk = new Set(readdirSync(dir));
  let bad = 0;
  for (const [name, env] of outputs) {
    if (!onDisk.delete(name)) { console.error("frozen report missing: " + name); bad++; continue; }
    const got = run(env), want = readFileSync(join(dir, name), "utf8");
    if (got === want) continue;
    bad++;
    const g = got.split("\n"), w = want.split("\n");
    const i = g.findIndex((l, k) => l !== w[k]);
    console.error("DRIFT  " + name + "  first at line " + (i + 1) +
      "\n    want  " + JSON.stringify(w[i]) + "\n    got   " + JSON.stringify(g[i]));
  }
  for (const stale of onDisk) { console.error("frozen report has no producer: " + stale); bad++; }
  return bad;
};

const write = (dir, outputs) => {
  mkdirSync(dir, { recursive: true });
  for (const [name, env] of outputs) writeFileSync(join(dir, name), run(env));
};

const argOf = (flag) => {
  const i = process.argv.indexOf(flag);
  return i < 0 ? null : process.argv[i + 1];
};
const all = new Map([...productOutputs, ...fixtureOutputs]);
const snapshot = argOf("--snapshot"), against = argOf("--against");

if (snapshot) {
  write(isAbsolute(snapshot) ? snapshot : join(root, snapshot), all);
  console.log("snapshot written: " + all.size + " reports over both trees");
} else if (against) {
  const bad = compare(isAbsolute(against) ? against : join(root, against), all);
  console.log(bad ? "\n" + bad + " report(s) drifted" : "semantic output identical across " + all.size + " reports");
  process.exit(bad ? 1 : 0);
} else if (process.argv.includes("--update")) {
  write(join(here, "golden"), fixtureOutputs);
  console.log("fixture reports rewritten: " + fixtureOutputs.size);
} else {
  const bad = compare(join(here, "golden"), fixtureOutputs);
  console.log(bad ? "\n" + bad + " fixture report(s) drifted"
    : "fixture reports identical across " + fixtureOutputs.size + " outputs");
  process.exit(bad ? 1 : 0);
}
