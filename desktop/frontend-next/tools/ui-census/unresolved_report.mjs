// How the unresolved-cause decomposition is printed. Formatting only: what the
// numbers are is unresolved_causes.mjs's, and nothing here decides anything.
import { stateR2 } from "./verdicts.mjs";
import * as b0 from "./unresolved_causes.mjs";

const desc = (m) => [...m.entries()].sort((a, b) => b[1] - a[1] || String(a[0]).localeCompare(String(b[0])));

function reportUnresolvedCauses() {
  const N = b0.population.length;
  console.log("\nSeal-B0  unresolved cause decomposition");
  console.log("  population    roots whose verdict is UNRESOLVED and whose action the product does not declare");
  console.log("  excludes      MUTATION roots (all named), and READ_ONLY roots (vocabulary debt, not measurement)");
  console.log("\n  Five numbers, never one:");
  console.log("    code sites      distinct (file, scope, name) a fix would be written at");
  console.log("    cause instances distinct source-cause identities");
  console.log("    (root, cause)   how many times a root's openness rests on one of them");
  console.log("    roots           distinct roots carrying at least one");
  console.log("    payoff          roots whose every source cause is covered once this selector is closed");
  console.log("\n  UNDECLARED + UNRESOLVED roots   " + N + "   of " + stateR2.length + " certified");

  const obs = new Map();
  for (const r of b0.rows) for (const [k, n] of r.observed) {
    if (!obs.has(k)) obs.set(k, { occ: 0, roots: 0 });
    obs.get(k).occ += n; obs.get(k).roots++;
  }
  console.log("\n  as observed, by axis                             occurrences   roots");
  for (const [k, v] of [...obs.entries()].sort((a, b) => b[1].roots - a[1].roots || a[0].localeCompare(b[0]))) {
    console.log("    " + k.padEnd(48) + String(v.occ).padStart(7) + String(v.roots).padStart(8));
  }
  console.log("  An axis reaching an effect whose setup is open carries no cause of its own;");
  console.log("  the causes below are what those projections reduce to, deduped by identity.");

  console.log("\n  fix selectors                          sites  instances  (root,cause)  roots  payoff");
  const sel = new Set(b0.rows.flatMap((r) => [...r.selectors]));
  const rank = [...sel].map((k) => {
    const d = b0.denominators((s) => s.selector === k);
    return { k, ...d, payoff: b0.rows.filter((r) => r.selectors.size === 1 && r.selectors.has(k)).length };
  }).sort((a, b) => b.roots - a.roots || b.payoff - a.payoff);
  for (const r of rank) {
    console.log("    " + r.k.padEnd(36) + String(r.sites).padStart(5) + String(r.instances).padStart(11) +
      String(r.edges).padStart(14) + String(r.roots).padStart(7) + String(r.payoff).padStart(8));
  }
  console.log("    " + "(no single selector closes these)".padEnd(36) +
    String(b0.rows.filter((r) => r.selectors.size > 1).length).padStart(66 - 36 + 15));

  const sets = new Map();
  for (const r of b0.rows) {
    const k = [...r.selectors].sort().join(" + ");
    sets.set(k, (sets.get(k) ?? 0) + 1);
  }
  console.log("\n  selector-set histogram (a root closes only when every member closes)");
  for (const [k, v] of desc(sets)) console.log("    " + String(v).padStart(4) + "  " + k);

  console.log("\n  cumulative closure, greedy. Set containment over one complete fact set,");
  console.log("  so a later cut's gain is not an earlier cut's leftovers reappearing. Still");
  console.log("  an upper bound: closing a selector means closing every instance of it, and");
  console.log("  a resolved unknown may reveal a write rather than a clean path.");
  for (const s of b0.greedy()) {
    console.log("    +" + String(s.gain).padStart(3) + "   " + String(s.total).padStart(4) + "/" + N + "   " + s.selector);
  }

  // The two largest selectors are not yet one job each. What is inside them
  // decides whether a cut exists at all, so they are censused rather than
  // ranked on their totals.
  for (const which of ["DEP_PROVENANCE", "CALLEE_UNRESOLVED", "UNPLACED_RECEIVER_METHOD"]) {
    const m = new Map();
    for (const r of b0.rows) for (const s of r.sources.values()) {
      if (s.selector !== which) continue;
      const key = which === "DEP_PROVENANCE" ? s.id.split("@")[0]
        : which === "CALLEE_UNRESOLVED" ? (s.id.split("#").pop() ?? "?")
        : (s.base ?? "?");
      if (!m.has(key)) m.set(key, { edges: 0, sites: new Set() });
      m.get(key).edges++; m.get(key).sites.add(s.id);
    }
    if (!m.size) continue;
    console.log("\n  " + which + ", by what could not be read      sites  (root,cause)");
    for (const [k, v] of [...m.entries()].sort((a, b) => b[1].edges - a[1].edges).slice(0, 12)) {
      console.log("    " + k.padEnd(40) + String(v.sites.size).padStart(5) + String(v.edges).padStart(14));
    }
  }
}

export { reportUnresolvedCauses };
