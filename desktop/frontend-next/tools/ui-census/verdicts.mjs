// State-R2: the join. Four axes, worst wins, and a MUTATION carries a witness.
import { sourceOf } from "./causes.mjs";
import { roots } from "./roots.mjs";
import { stateR1 } from "./state_dependency.mjs";
import { stateR1c } from "./state_identity.mjs";
import { stateR1b } from "./state_lifecycle.mjs";

// This adds no analysis. Every fact it uses was computed above: what a handler
// reaches directly, what a state write retriggers, what a prop's identity does
// when a parent renders, and what a component's arrival or departure runs. R2
// projects them onto one verdict per interaction and keeps the reason.
//
// The rule, in this order and no other: a proven mutation on any reachable axis
// wins, because the question is may-mutate and another axis not knowing does
// not erase a witness. Otherwise any reachable axis that is open makes the
// whole verdict open — one vote of read-only and one of unknown is unknown.
// Read-only is what is left when every reachable axis is proven and no causal
// edge is open, and it means exactly that: no host mutation on the direct call,
// dependency, cross-component identity and lifecycle paths as modelled here.
// It does not mean the interaction does nothing — local state, focus and
// scrolling are not host mutations and are not what this measures.
const RANK = { MUTATION: 3, UNRESOLVED: 2, READ_ONLY: 1 };
const stateR2 = (() => {
  const worse = (a, b) => (RANK[b] > RANK[a] ? b : a);
  const rows = [];
  for (const r of roots) {
    const axes = { direct: r.verdict, dependency: "READ_ONLY", identity: "READ_ONLY", lifecycle: "READ_ONLY" };
    const open = new Map();
    const cells = [];
    // The source causes this root's openness reduces to, deduped by identity.
    // A projection is not one of them: its own sources are, which is why an
    // axis reaching an open effect setup adds nothing here of its own.
    const causes = new Map();
    let witness = null, family = null, danglingProjections = 0;
    const take = (axis, list) => {
      for (const c of list ?? []) {
        open.set(axis + ": " + c.kind, (open.get(axis + ": " + c.kind) ?? 0) + 1);
        if (c.projection && !c.sources.length) danglingProjections++;
        for (const src of c.sources) causes.set(src.source, src);
      }
    };
    if (r.verdict === "MUTATION") { witness = "handler -> " + (r.trailOf ?? r.mutates.join("+")); family = "direct call"; }
    for (const o of r.open ?? []) {
      open.set("direct: " + o.kind, (open.get("direct: " + o.kind) ?? 0) + 1);
      const src = sourceOf(o);
      causes.set(src.source, src);
    }

    for (const w of r.stateWrites) {
      const keys = stateR1.cellsForWrite(w);
      if (!keys) {
        axes.dependency = worse(axes.dependency, "UNRESOLVED");
        open.set("dependency: state write not placed", (open.get("dependency: state write not placed") ?? 0) + 1);
        continue;
      }
      for (const k of keys) {
        cells.push(k);
        const dep = stateR1.verdicts.get(k), id = stateR1c.verdicts.get(k), life = stateR1b.verdicts.get(k);
        axes.dependency = worse(axes.dependency, dep?.verdict ?? "UNRESOLVED");
        axes.identity = worse(axes.identity, id?.verdict ?? "UNRESOLVED");
        axes.lifecycle = worse(axes.lifecycle, life?.verdict ?? "READ_ONLY");
        if (dep?.verdict === "UNRESOLVED") take("dependency", dep.openCauses);
        if (id?.verdict === "UNRESOLVED") take("identity", id.openCauses);
        if (life?.verdict === "UNRESOLVED") take("lifecycle", life.openCauses);
        if (!witness && life?.verdict === "MUTATION") {
          const e = stateR1.effects.get(life.effect);
          witness = "write " + k.split("#").pop() + " -> " + life.why + " -> " + (life.effect ?? "?") +
            " -> " + (e ? [...e.setup.direct, ...(e.cleanup?.direct ?? [])].join("+") : "?");
          family = life.family;
        }
        if (!witness && (dep?.verdict === "MUTATION" || id?.verdict === "MUTATION")) {
          const hit = dep?.verdict === "MUTATION" ? dep : id;
          // The witness field, not the display line: a MUTATION's evidence is
          // owned, not parsed back out of a sentence written for a human.
          const ek = hit.witness ?? null;
          const e = ek ? stateR1.effects.get(ek) : null;
          const local = dep?.verdict === "MUTATION";
          witness = "write " + k.split("#").pop() + " -> " + (local ? "dependency" : "cross-component prop") +
            " -> " + (ek ?? hit.why) + " -> " + (e ? [...e.setup.direct, ...(e.cleanup?.direct ?? []), ...e.setup.scheduled].join("+") : "?");
          // Whether a cross-component edge carried the value or a rebuilt
          // identity is not recorded on the path; saying which would mean
          // teaching R1c to keep one, and that is upstream work, not a join.
          family = e && e.setup.scheduled.length ? "scheduled"
            : e && e.cleanup && !e.cleanup.unresolved && e.cleanup.direct.length ? "dependency cleanup"
            : local ? "dependency retrigger" : "cross-component prop";
        }
      }
    }
    const verdict = [axes.direct, axes.dependency, axes.identity, axes.lifecycle].reduce(worse, "READ_ONLY");
    rows.push({ root: r, axes, verdict, witness, family, open: [...open.keys()],
      openCounts: [...open], causes: [...causes.values()], danglingProjections, cells });
  }
  return rows;
})();

export { RANK, stateR2 };
