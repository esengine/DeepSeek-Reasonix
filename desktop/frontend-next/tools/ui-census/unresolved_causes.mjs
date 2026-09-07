// Seal-B0: why the roots that might write have no identity yet.
//
// Every cause is read from the canonical set the axes now record. Nothing here
// parses a reason back out of a sentence, and nothing re-derives a cause: a
// projection was already reduced to its sources where it was produced, so what
// arrives is the set a fix would be written against.
//
// The unit of ranking is a fix selector, not an error family. `unknown-callee`
// is not something anyone can go and fix; "calls the platform's listener
// registration" was, and closing it took thirty roots out while leaving the
// rest of the family untouched.
import { membership } from "./gate.mjs";
import { stateR1 } from "./state_dependency.mjs";
import { stateR2 } from "./verdicts.mjs";

const population = stateR2.filter((x) => x.verdict === "UNRESOLVED" && membership.get(x.root) !== "NAMED");

// What a fix would be written against, decided from structure. A source whose
// selector needs a census of its own says so rather than being folded into a
// family that would then look bigger than any one PR could close.
const selectorOf = (src) => {
  const [mechanism, ...rest] = src.source.split(":");
  const id = rest.join(":");
  if (mechanism === "callee-unresolved") {
    // Three different jobs under one error family. A scheduler is a contract
    // that can be written, a promise continuation is a receiver-provenance
    // question the member's spelling may not answer, and the rest is neither.
    const selector = src.via?.scheduler ? "GLOBAL_" + src.via.scheduler.replace(/([a-z])([A-Z])/g, "$1_$2").toUpperCase()
      : /^member-name-only:\.(then|catch|finally)$/.test(src.via?.provenance ?? "") ? "PROMISE_CONTINUATION_UNPROVEN"
      : "OTHER_CALLEE_UNRESOLVED";
    return { family: mechanism, selector, id };
  }
  if (mechanism === "prop-source" && src.via?.shape) {
    return { family: mechanism, selector: "PROP_" + src.via.shape.replace(/-/g, "_").toUpperCase(), id };
  }
  if (mechanism !== "unknown-callee") return { family: mechanism, selector: mechanism.toUpperCase().replace(/-/g, "_"), id };
  const [path, comp, name] = id.split("#");
  const base = (name ?? "").includes(".") ? name.slice(0, name.lastIndexOf(".")).split(".")[0] : null;
  if (base === null) return { family: mechanism, selector: "LOCAL_DECLARATION", id };
  const b = stateR1.resolveBinding(path, comp === "?" ? null : comp, base);
  const selector = b.cells.size || b.memos.size ? "LOCAL_VALUE_METHOD"
    : b.outer.length ? "OUTER_BINDING_METHOD" : "UNPLACED_RECEIVER_METHOD";
  return { family: mechanism, selector, id, base };
};

const rows = population.map((row) => {
  const observed = new Map();
  for (const [k, n] of row.openCounts) observed.set(k, (observed.get(k) ?? 0) + n);
  const sources = new Map();
  for (const s of row.causes) sources.set(s.source, { ...s, ...selectorOf(s) });
  return { row, observed, sources, selectors: new Set([...sources.values()].map((s) => s.selector)) };
});

// Roots whose every source cause is covered by `set`. A set operation over one
// complete fact set, so the order causes were discovered in cannot move it, and
// so a second cut's gain is not the first cut's leftovers reappearing.
const closedBy = (set) => rows.filter((r) => [...r.selectors].every((m) => set.has(m))).length;

const greedy = () => {
  const all = [...new Set(rows.flatMap((r) => [...r.selectors]))].sort();
  const taken = new Set(), steps = [];
  for (;;) {
    let best = null, gain = 0;
    const before = closedBy(taken);
    for (const m of all) {
      if (taken.has(m)) continue;
      const n = closedBy(new Set([...taken, m])) - before;
      if (n > gain) { gain = n; best = m; }
    }
    if (!best) break;
    taken.add(best);
    steps.push({ selector: best, gain, total: closedBy(taken) });
  }
  return steps;
};

// Five numbers, never one. Each answers a different question and the earlier
// drafts of this table read 103 code sites for eleven by conflating the second
// with the first.
const denominators = (pick) => {
  const sites = new Set(), instances = new Set();
  let edges = 0;
  const roots = new Set();
  for (const r of rows) for (const s of r.sources.values()) {
    if (!pick(s)) continue;
    edges++;
    sites.add(s.id);
    instances.add(s.source);
    roots.add(r.row.root);
  }
  return { sites: sites.size, instances: instances.size, edges, roots: roots.size };
};

export { closedBy, denominators, greedy, population, rows, selectorOf };
