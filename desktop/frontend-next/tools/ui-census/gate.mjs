// The invariants this census exists to hold, computed from the same structures
// the reports print. A number is checked here and printed there: one expression,
// so a gate and a report cannot drift into disagreeing about the same fact.
//
// Only settled invariants live here. The two open seals — every input that
// might write having an identity (B), and the reverse direction over mutation
// capabilities — are open on purpose and are reported, never enforced: a gate
// that fails on known-open debt is a gate someone turns off.
import { SRC } from "./source.mjs";
import { capabilities, implFiles } from "./capabilities.mjs";
import { mutating } from "./sinks.mjs";
import { refused, roots, uncertified } from "./roots.mjs";
import { stateR2 } from "./verdicts.mjs";
import { REGISTRY, stateActions } from "./actions.mjs";
import { transport } from "./transport.mjs";
import { classify } from "./classify.mjs";
import { facts as schedulerCalls } from "./schedulers.mjs";
import { alias, bodyOf, renderTargetsOf } from "./component_identity.mjs";
import { renders } from "./sites.mjs";
import { shadowedByLaterSpread } from "./prop_sources.mjs";
import { formals } from "./flow.mjs";
import { flat, isCall, productFiles, trees, walkStack } from "./source.mjs";
import { ownerOf } from "./symbols.mjs";
import { capabilityMutates } from "./types.mjs";
import { byNode as continuations, rootProvenance } from "./promises.mjs";

const membership = new Map(stateActions.rows.map((x) => [x.root, x.state]));
const declaredIds = new Set(stateActions.actions.map((a) => a.id));

const noVerdict = stateR2.filter((x) => !/^(MUTATION|UNRESOLVED|READ_ONLY)$/.test(x.verdict)).length;
const mutNoWitness = stateR2.filter((x) => x.verdict === "MUTATION" && !x.witness).length;
const roNotClosed = stateR2.filter((x) => x.verdict === "READ_ONLY" && x.open.length).length;
const mutUnnamed = stateR2.filter((x) => x.verdict === "MUTATION" && membership.get(x.root) !== "NAMED").length;
const unrUnnamed = stateR2.filter((x) => x.verdict === "UNRESOLVED" && membership.get(x.root) !== "NAMED").length;
const orphans = REGISTRY ? [...REGISTRY.keys()].filter((id) => !declaredIds.has(id)).length : null;
const strays = REGISTRY ? [...declaredIds].filter((id) => !REGISTRY.has(id)).length : null;

// Conservation: every capability the wrapper graph reached has to land on an
// endpoint site, a dynamic endpoint or an unreadable fetch. Nothing may simply
// stop being reachable, so this runs on every invocation rather than only under
// the probe that prints it.
const conservation = (() => {
  const covered = new Set([...transport.sites, ...transport.inlineSites.filter((s) => s.path !== null)].map((s) => s.owner));
  const dyn = new Set(transport.dynamic.map((d) => d.owner));
  const unreadable = new Set(transport.inlineSites.filter((s) => s.path === null).map((s) => s.owner));
  const old = [...capabilities].filter((c) => capabilityMutates(c));
  const bucket = { site: [], dynamic: [], unreadable: [], lost: [] };
  for (const c of old) {
    const impls = [...implFiles].map((f) => f + "#" + c).filter((k) => mutating.has(k));
    const reaches = (set) => impls.some((k) => set.has(k)) ||
      impls.some((k) => [...set].some((o) => o.endsWith("#" + c)));
    if (reaches(covered)) bucket.site.push(c);
    else if (reaches(dyn)) bucket.dynamic.push(c);
    else if (reaches(unreadable)) bucket.unreadable.push(c);
    else bucket.lost.push(c);
  }
  return { old, ...bucket };
})();

// A mixed action must still show every verdict it joined: a member root's
// verdict is read from R2 and never written back.
const mixedKept = stateActions.actions.filter((a) => a.mixed)
  .every((a) => a.roots.every((r, i) => stateR2.find((x) => x.root === r).verdict === a.verdicts[i]));

// A platform scheduler runs the callback it is handed, and no contract says so
// yet, so a callback passed by name is invisible: the callee is on the platform
// list and the argument is nobody's callee. Measured rather than assumed — the
// hole is empty today, and this is what fails the day someone puts a write in
// it. Writing the contract is the fix; holding the hole at empty is what makes
// deferring it honest.
const schedulerHidesAWrite = schedulerCalls.filter((f) => {
  if (f.visibleToday || !f.eligible || !f.arg) return false;
  const out = { mutates: new Set(), open: [], stateWrites: new Set(), scheduled: new Set(),
    directMutations: new Set(), scheduledMutations: new Set() };
  classify(f.arg, f.path, f.comp, 0, new Set(), out, [f.path + ":" + f.line]);
  return out.mutates.size > 0;
});

// The two directions of the component identity map must be inverses. A reader
// that resolves an alias and one that indexes by body are the same fact seen
// from two sides, and this series has three times found them disagreeing —
// each time because one of them rebuilt identity from a name.
const identityNotInvertible = [...alias.keys()].filter((a) => {
  const body = bodyOf(a);
  return body === a || !renderTargetsOf(body).includes(a);
});

// Which formal props are followed is decided by the prop's spelling: only a
// name shaped `onX` reaches the actual a parent passes. Measured rather than
// argued — twelve calls are refused today and none of them reaches a write, so
// the heuristic costs precision and not soundness. This is what fails on the
// day that stops being true, which is the day it becomes worth removing.
const propHeuristicHidesAWrite = [];
for (const [path, tree] of trees) {
  if (!productFiles.has(path)) continue;
  walkStack(tree, (n, stack) => {
    if (!isCall(n)) return;
    const callee = flat(n.callee);
    if (callee.includes(".") || /^on[A-Z]/.test(callee)) return;
    const comp = ownerOf(stack);
    const slot = formals.get(path + "#" + comp)?.get(callee);
    if (!slot || slot.startsWith("@")) return;
    for (const site of renders.get(path + "#" + comp) ?? []) {
      const passed = site.props.get(slot);
      if (!passed) continue;
      const out = { mutates: new Set(), open: [], stateWrites: new Set(), scheduled: new Set(),
        directMutations: new Set(), scheduledMutations: new Set() };
      classify(passed.type === "JSXExpressionContainer" ? passed.expression : passed,
        site.file, site.host, 0, new Set(), out, [site.file + ":" + site.line]);
      if (out.mutates.size) propHeuristicHidesAWrite.push(path + "#" + comp + "." + callee);
    }
  });
}

// A continuation is an execution edge into a callback, and this analyzer takes
// one only where the receiver was proven to be a Promise by a declared return
// type. Every other source above that is a continuation on something already
// proven, so the bottom of the chain is the one place a new source could enter.
// The day a member's spelling buys that edge again, this is what says so.
const continuationWithoutProvenance = [...continuations.values()]
  .filter((r) => !r.on || rootProvenance(r.on) !== "CERTIFIED_API_RETURN");

// A projection with no source is a debt with nothing behind it: it would enter
// the ranking as a mechanism of its own, which is the error the reduction
// exists to prevent. An axis reaching an open effect setup must always be able
// to say what inside that effect is open.
const danglingProjections = stateR2.reduce((n, x) => n + (x.danglingProjections ?? 0), 0);

const denominatorSealed = uncertified.length === 0 && refused.length === 0 &&
  noVerdict === 0 && mutNoWitness === 0 && roNotClosed === 0;

// What CI holds. Each is a fact about structure, not a threshold someone chose.
const INVARIANTS = [
  ["REFUSED_DECLARATION", refused.length, "an action declared on a non-input, or one an element cannot place"],
  ["UNCERTIFIED_SOURCE", uncertified.length, "an interaction source the walk never placed: a hole in the denominator"],
  ["ROOT_WITHOUT_VERDICT", noVerdict, "a certified root the join produced no effect verdict for"],
  ["MUTATION_WITHOUT_WITNESS", mutNoWitness, "a MUTATION verdict carrying no positive witness"],
  ["READ_ONLY_WITH_OPEN_EDGE", roNotClosed, "a READ_ONLY verdict with a reachable open edge"],
  ["UNDECLARED_MUTATION", mutUnnamed, "an input proven to write the host that the product names no action for"],
  ["REGISTRY_ORPHAN", orphans ?? 0, "a registry action no production root drives"],
  ["REGISTRY_STRAY", strays ?? 0, "a rendered action id the registry does not declare"],
  ["CAPABILITY_SILENTLY_LOST", conservation.lost.length, "a mutating capability that reaches no endpoint site at all"],
  ["PROP_SOURCE_IGNORES_A_LATER_SPREAD", shadowedByLaterSpread.length,
    "a prop taken from an explicit attribute with an unproven spread to its right: " +
    shadowedByLaterSpread.join(" ")],
  ["COMPONENT_IDENTITY_NOT_INVERTIBLE", identityNotInvertible.length,
    "a certified render alias whose body does not list it back: " + identityNotInvertible.join(" ")],
  ["PROP_HEURISTIC_HIDES_A_WRITE", propHeuristicHidesAWrite.length,
    "a formal prop refused by the onX spelling rule whose actual reaches a write: " +
    [...new Set(propHeuristicHidesAWrite)].join(" ")],
  ["SCHEDULER_HIDES_A_WRITE", schedulerHidesAWrite.length,
    "a callback handed to a platform scheduler by name reaches a write the walk cannot see: " +
    schedulerHidesAWrite.map((f) => f.path + ":" + f.line).join(" ")],
  ["PROMISE_CONTINUATION_WITHOUT_PROMISE_PROVENANCE", continuationWithoutProvenance.length,
    "a .then/.catch/.finally the walk executes whose receiver no declared return type proves: " +
    continuationWithoutProvenance.map((r) => r.edgeAt).join(" ")],
  ["PROJECTION_CAUSE_WITHOUT_SOURCE", danglingProjections, "an open cause that is a projection of another and cannot name it"],
  ["MIXED_ACTION_REWRITTEN", mixedKept ? 0 : 1, "a mixed action whose member verdict was rewritten by the join"],
];

// A corpus may intend to break an invariant — that is what the fixture tree is
// for, and asserting "nothing is broken" there would deny it its purpose. So
// each tree declares which invariants it breaks and by how much, and the gate
// holds it to exactly that set: a fixture that stops refusing fails here too.
// Declared per tree and closed, the same discipline the endpoint adjudication
// runs under, so an exemption cannot outlive the case it was written for.
const EXPECTED_BREAKS = {
  src: {},
  _fx: {
    REFUSED_DECLARATION: [1, "fixture Z declares an action on a resize: refusing it is the case"],
    PROP_HEURISTIC_HIDES_A_WRITE: [1, "fixture H1 is the case: a called prop the spelling rule refuses, whose actual writes"],
    UNCERTIFIED_SOURCE: [1, "fixture E3 registers on a receiver nothing proves to be a DOM target: a hole is the honest answer, and refusing to guess is the case"],
    UNDECLARED_MUTATION: [29, "the fixture corpus names actions only where identity is the subject"],
  },
};

function enforce() {
  const expected = EXPECTED_BREAKS[SRC] ?? {};
  let bad = 0;
  for (const [name, n, why] of INVARIANTS) {
    const want = expected[name]?.[0] ?? 0;
    if (n === want) continue;
    bad++;
    console.error("INVARIANT " + name + " = " + n + (want ? ", declared " + want : "") + "   " +
      (want ? expected[name][1] : why));
  }
  for (const name of Object.keys(expected)) {
    if (INVARIANTS.some(([k]) => k === name)) continue;
    console.error("declared break names no invariant: " + name);
    bad++;
  }
  if (bad) process.exitCode = 1;
  return bad === 0;
}

export { conservation, danglingProjections, denominatorSealed, enforce, INVARIANTS, membership, mixedKept,
  mutNoWitness, mutUnnamed, noVerdict, orphans, roNotClosed, strays, unrUnnamed };
