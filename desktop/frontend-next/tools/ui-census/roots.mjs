// The certified interaction roots — the denominator every verdict is a share of.
import { census } from "./certified.mjs";
import { classify } from "./classify.mjs";

// The certification is the universe; this file adds what a source census cannot
// answer — what each root reaches — and reads that universe rather than
// deciding it a second time.
const roots = census.roots;
const uncertified = census.uncertified;
const nonUser = census.nonUser;
const refused = census.refused;

// One consumption interface: every root, whatever installed it, is a callback
// whose reach decides the verdict.
for (const r of roots) {
  const out = { mutates: new Set(), open: [], stateWrites: new Set(), scheduled: new Set(), actuals: new Set(), directMutations: new Set(), scheduledMutations: new Set() };
  if (r.callback) classify(r.callback, r.path, r.comp, 0, new Set(), out, [r.path + ":" + r.line]);
  r.verdict = out.mutates.size ? "MUTATION" : out.open.length ? "UNRESOLVED" : "READ_ONLY";
  r.mutates = [...out.mutates];
  r.stateWrites = [...out.stateWrites];
  r.scheduled = [...out.scheduled];
  r.trailOf = out.trailOf;
  r.open = out.open;
  r.actuals = [...(out.actuals ?? [])];
}
const sites = roots.filter((r) => r.kind === "jsx-handler");

export { census, nonUser, refused, roots, sites, uncertified };
