import { mutating, sinks } from "./sinks.mjs";
import { byVerdict, offstage, reachable, unnamedMutations, unresolvedSinks } from "./derived.mjs";
import { nonUser, refused, roots, sites, uncertified } from "./roots.mjs";
import { SRC, productFiles } from "./source.mjs";
import { effectRows } from "./effects.mjs";
import { facts as registrationFacts } from "./registrations.mjs";
import { stateR1 } from "./state_dependency.mjs";
import { reportUnresolvedCauses } from "./unresolved_report.mjs";
import { stateR1b } from "./state_lifecycle.mjs";
import { stateR1c } from "./state_identity.mjs";
import { stateR2 } from "./verdicts.mjs";
import { capabilities, implFiles } from "./capabilities.mjs";
import { capabilityMutates } from "./types.mjs";
import { REGISTRY, stateActions } from "./actions.mjs";
import { transport } from "./transport.mjs";
import { NOT_A_MUTATION } from "./endpoints.mjs";
import { maxDepth as promiseDepth, sites as promiseSites } from "./promises.mjs";
import { conservation, denominatorSealed, membership, mixedKept, mutNoWitness,
  mutUnnamed, noVerdict, orphans, roNotClosed, strays, unrUnnamed } from "./gate.mjs";

console.log("Raw mutation sinks                 " + sinks.length);
console.log("  product-reachable                " + reachable.length);
console.log("  non-product-reachable            " + offstage.length + (offstage.length ? "   " + [...new Set(offstage.map((s) => s.path))].join(", ") : ""));
console.log("  verb not statically readable     " + unresolvedSinks.length);
console.log("Mutation-capable functions         " + mutating.size);
console.log("");
console.log("Interaction sites (product only)    " + sites.length);
console.log("  MUTATION                          " + byVerdict("MUTATION").length + "   (unnamed " + unnamedMutations.length + ")");
console.log("  READ_ONLY / LOCAL_UI              " + byVerdict("READ_ONLY").length);
console.log("  UNRESOLVED                        " + byVerdict("UNRESOLVED").length);

if (process.env.SHOW === "unresolved") {
  const byKind = new Map();
  for (const s of byVerdict("UNRESOLVED")) for (const o of s.open) {
    if (!byKind.has(o.kind)) byKind.set(o.kind, []);
    byKind.get(o.kind).push(s.path.replace("src/ui/", "") + ":" + s.line + "   " + o.at);
  }
  console.log("\nUNRESOLVED by kind");
  for (const [k, v] of [...byKind.entries()].sort((a, b) => b[1].length - a[1].length)) {
    console.log("\n" + k + "  (" + v.length + ")");
    for (const x of [...new Set(v)].slice(0, 8)) console.log("   " + x);
  }
}
if (process.env.SHOW === "sinks") {
  console.log("");
  for (const s of sinks) console.log((productFiles.has(s.path) ? "product " : "offstage") + "  " + s.path + ":" + s.line + "  " + s.how + "  owner=" + s.owner);
}
if (process.env.SHOW === "unnamed") {
  console.log("");
  for (const s of unnamedMutations) console.log(s.path.replace("src/ui/", "") + ":" + s.line + "   " + s.mutates.join(", ") + (s.label ? "   " + s.label : ""));
}

if (process.env.PROBE === "effects") {
  const rows = effectRows;
  const n = (f) => rows.filter(f).length;
  console.log("\nEffects total                        " + rows.length);
  console.log("  setup: direct mutation             " + n((r) => r.setup.direct.length));
  console.log("  setup: schedules a mutation        " + n((r) => r.setup.scheduled.length));
  console.log("  registers an interaction           " + n((r) => r.registers));
  console.log("  setup: open                        " + n((r) => r.setup.open > 0));
  console.log("\n  returning a cleanup                " + n((r) => r.cleanup));
  console.log("    cleanup: direct mutation         " + n((r) => r.cleanup && !r.cleanup.unresolved && r.cleanup.direct.length));
  console.log("    cleanup: schedules a mutation    " + n((r) => r.cleanup && !r.cleanup.unresolved && r.cleanup.scheduled.length));
  console.log("    cleanup: open                    " + n((r) => r.cleanup && !r.cleanup.unresolved && r.cleanup.open > 0));
  console.log("    cleanup: not statically readable " + n((r) => r.cleanup?.unresolved));
  console.log("\n  dependency shape");
  for (const k of ["EVERY_COMMIT", "MOUNT_ONLY", "EXPLICIT_DEPS", "UNRESOLVED_DEPS"]) {
    console.log("    " + k.padEnd(18) + n((r) => r.deps.kind === k));
  }
  console.log("\n  effects that mutate, and how they are triggered:");
  for (const r of rows.filter((x) => x.setup.direct.length || x.setup.scheduled.length ||
      (x.cleanup && !x.cleanup.unresolved && (x.cleanup.direct.length || x.cleanup.scheduled.length)))) {
    console.log("    " + r.path.replace("src/ui/", "") + ":" + r.line + "  " + r.deps.kind +
      " [" + r.deps.names.join(", ") + "]  setup=" + r.setup.direct.join("+") +
      (r.cleanup && !r.cleanup.unresolved && r.cleanup.direct.length ? "  cleanup=" + r.cleanup.direct.join("+") : ""));
  }
}

{
  const by = (k) => roots.filter((r) => r.verdict === k);
  console.log("\nInteraction roots (certified)      " + roots.length);
  console.log("  jsx-handler                      " + roots.filter((r) => r.kind === "jsx-handler").length);
  console.log("  command-chord                    " + roots.filter((r) => r.kind === "command-chord").length);
  console.log("  dom-event                        " + roots.filter((r) => r.kind === "dom-event").length);
  console.log("  MUTATION                         " + by("MUTATION").length + "   (unnamed " + by("MUTATION").filter((r) => !r.named).length + ")");
  console.log("  READ_ONLY / LOCAL                " + by("READ_ONLY").length);
  console.log("  UNRESOLVED                       " + by("UNRESOLVED").length);
  const resting = by("READ_ONLY").filter((r) => r.stateWrites.length);
  console.log("    of which read-only only until the effect closure over their\n    React state is checked   " + resting.length);
  console.log("\nDOM registrations not certified as user input  " + uncertified.length);
  for (const [w, n] of Object.entries(uncertified.reduce((m, u) => ({ ...m, [u.why]: (m[u.why] ?? 0) + 1 }), {}))) {
    console.log("  " + w.padEnd(24) + " " + n);
  }
  if (process.env.PROBE === "verdicts") {
    for (const r of [...roots].sort((a, b) => (a.path + a.line).localeCompare(b.path + b.line))) {
      console.log("  " + r.path.split("/").pop() + ":" + r.line + "  " + r.verdict + "  " + r.mutates.join(","));
    }
  }
  if (process.env.PROBE === "oracle") {
    const rows = roots.map((r) => [r.kind === "dom-event" ? "dom-event:" + r.event : r.kind,
      r.path + ":" + r.line, r.verdict, [...r.mutates].sort().join("+")].join("|"));
    for (const x of rows.sort()) console.log(x);
  }
  if (process.env.PROBE === "domroots") {
    for (const r of roots.filter((x) => x.kind === "dom-event")) {
      console.log("  " + r.verdict.padEnd(11) + r.path.replace("src/ui/", "") + ":" + r.line + "  " + r.event + "   " + r.mutates.join(", "));
    }
  }
}

if (process.env.PROBE === "apis") {
  const m = new Map();
  for (const r of roots) for (const o of r.open) {
    if (o.kind !== "callee-unresolved") continue;
    m.set(o.provenance, (m.get(o.provenance) ?? 0) + 1);
  }
  let strong = 0, weak = 0;
  console.log("");
  for (const [k, v] of [...m.entries()].sort((a, b) => b[1] - a[1])) {
    console.log("  " + String(v).padStart(4) + "  " + k);
    if (/^(import|global|receiver)-proven/.test(k)) strong += v; else weak += v;
  }
  console.log("");
  console.log("  provenance strong enough for a contract   " + strong);
  console.log("  spelling only                             " + weak);
}

// A callback parameter is bound by whoever calls the callback, and two callers
// state what they bind it to: an iteration hands over an element of the
// receiver, and a state updater hands over the cell's own value. Both are
// written in the source; neither is a local declaration, so without this they
// read as names from nowhere and blanket the component.
{
  const v = [...stateR1.verdicts.values()];
  const n = (k) => v.filter((x) => x.verdict === k).length;
  console.log("\nState cells (product)              " + stateR1.cells.size);
  console.log("  memo hooks that can carry one    " + stateR1.memos.size);
  console.log("  retrigger at least one effect    " + v.filter((x) => x.effects).length);
  console.log("  MUTATION                         " + n("MUTATION"));
  console.log("  UNRESOLVED                       " + n("UNRESOLVED"));
  console.log("  READ_ONLY                        " + n("READ_ONLY"));
  console.log("  useState not readable as a pair  " + stateR1.unreadableCells.length);
  console.log("  effect/memo deps that are neither state nor memo here  " + stateR1.outerDeps.length +
    "   (a prop or a module binding: another component's state, which this relation does not follow)");

  // The suspended roots, joined to what their writes reach. Reported, not
  // applied: upgrading a root's verdict is the next pass, and a preview that
  // silently became the verdict would be the same mistake twice.
  const suspended = roots.filter((r) => r.verdict === "READ_ONLY" && r.stateWrites.length);
  const tally = { MUTATION: 0, UNRESOLVED: 0, READ_ONLY: 0, UNMAPPED: 0 };
  for (const r of suspended) {
    const seen = [];
    for (const w of r.stateWrites) {
      const keys = stateR1.cellsForWrite(w);
      if (!keys) { seen.push("UNMAPPED"); continue; }
      for (const k of keys) seen.push(stateR1.verdicts.get(k)?.verdict ?? "UNMAPPED");
    }
    tally[seen.includes("MUTATION") ? "MUTATION" : seen.includes("UNMAPPED") ? "UNMAPPED"
      : seen.includes("UNRESOLVED") ? "UNRESOLVED" : "READ_ONLY"]++;
  }
  console.log("\n  suspended roots, by what their state writes reach (preview, not applied)");
  for (const k of ["MUTATION", "UNRESOLVED", "READ_ONLY", "UNMAPPED"]) console.log("    " + k.padEnd(12) + tally[k]);

  if (process.env.PROBE === "state") {
    console.log("");
    for (const x of [...stateR1.verdicts.values()].sort((a, b) => (a.cell.path + a.cell.line).localeCompare(b.cell.path + b.cell.line))) {
      console.log("  " + x.verdict.padEnd(11) + x.cell.path.replace("src/ui/", "") + ":" + x.cell.line + "  " +
        x.cell.comp + "." + x.cell.value + "  effects=" + x.effects + "  " + x.why);
    }
  }
}

{
  const v = [...stateR1b.verdicts.values()];
  const d = stateR1b.danger;
  console.log("\nLifecycle presence reachability");
  console.log("  components owning effects        " + stateR1b.byOwner.size);
  console.log("    a mount would mutate           " + d.filter((x) => x.mounts).length);
  console.log("    an unmount would mutate        " + d.filter((x) => x.unmounts).length);
  console.log("    a mount is not readable        " + d.filter((x) => x.mountOpen && !x.mounts).length);
  console.log("    an unmount is not readable     " + d.filter((x) => x.unmountOpen && !x.unmounts).length);
  const keyed = d.flatMap((x) => (stateR1b.sites.get(x.id) ?? []).filter((si) => si.key).map((si) => x.id + "  key at " + si.file.replace("src/ui/", "") + ":" + si.line));
  console.log("  dynamic keys on components in the danger set   " + keyed.length +
    "   (a new key is a new instance: unmount then mount)");
  console.log("  render targets that are not a component body  " + stateR1b.dynamicTargets.length);
  console.log("  components in the danger set whose presence is not readable  " + stateR1b.opaquePresence.length);
  console.log("\n  state cells that reach one, by lifecycle");
  console.log("    MUTATION                       " + v.filter((x) => x.verdict === "MUTATION").length);
  console.log("    UNRESOLVED                     " + v.filter((x) => x.verdict === "UNRESOLVED").length);

  const merged = new Map();
  for (const [k, x] of stateR1.verdicts) merged.set(k, { r1: x.verdict, r1b: "READ_ONLY", cell: x.cell });
  for (const [k, x] of stateR1b.verdicts) if (merged.has(k)) merged.get(k).r1b = x.verdict;
  const worst = (m) => (m.r1 === "MUTATION" || m.r1b === "MUTATION" ? "MUTATION" : m.r1 === "UNRESOLVED" || m.r1b === "UNRESOLVED" ? "UNRESOLVED" : "READ_ONLY");
  const t = { MUTATION: 0, UNRESOLVED: 0, READ_ONLY: 0 };
  for (const m of merged.values()) t[worst(m)]++;
  console.log("\n  state cells, dependency and lifecycle together");
  for (const k of ["MUTATION", "UNRESOLVED", "READ_ONLY"]) console.log("    " + k.padEnd(12) + t[k]);

  const suspended = roots.filter((r) => r.verdict === "READ_ONLY" && r.stateWrites.length);
  const tally = { MUTATION: 0, UNRESOLVED: 0, READ_ONLY: 0, UNMAPPED: 0 };
  for (const r of suspended) {
    const seen = [];
    for (const w of r.stateWrites) {
      const keys = stateR1.cellsForWrite(w);
      if (!keys) { seen.push("UNMAPPED"); continue; }
      for (const k of keys) seen.push(merged.has(k) ? worst(merged.get(k)) : "UNMAPPED");
    }
    tally[seen.includes("MUTATION") ? "MUTATION" : seen.includes("UNMAPPED") ? "UNMAPPED"
      : seen.includes("UNRESOLVED") ? "UNRESOLVED" : "READ_ONLY"]++;
  }
  console.log("\n  suspended roots, dependency and lifecycle together (still a candidate set:\n  cross-component prop dependencies are R1c and are not in this number)");
  for (const k of ["MUTATION", "UNRESOLVED", "READ_ONLY", "UNMAPPED"]) console.log("    " + k.padEnd(12) + tally[k]);

  if (process.env.PROBE === "life") {
    console.log("\n  danger set");
    for (const x of stateR1b.danger) {
      console.log("    " + x.id.replace("src/ui/", "") + "  " +
        [x.mounts && "mount-mutation", x.unmounts && "unmount-mutation", x.mountOpen && "mount-open", x.unmountOpen && "unmount-open"].filter(Boolean).join(" "));
    }
    console.log("\n  cells reached by lifecycle");
    for (const [k, x] of [...stateR1b.verdicts].sort()) console.log("    " + x.verdict.padEnd(11) + k.replace("src/ui/", "") + "   " + x.why.replace(/src\/ui\//g, ""));
    if (keyed.length) { console.log("\n  keyed sites in the danger set"); for (const x of keyed) console.log("    " + x.replace("src/ui/", "")); }
    if (stateR1b.dynamicTargets.length) { console.log("\n  not a component body"); for (const x of stateR1b.dynamicTargets.slice(0, 12)) console.log("    " + x); }
    if (stateR1b.opaquePresence.length) { console.log("\n  presence not readable"); for (const x of stateR1b.opaquePresence.slice(0, 12)) console.log("    " + x.id.replace("src/ui/", "") + "  " + x.why.join(" | ").replace(/src\/ui\//g, "")); }
  }
}

{
  const v = [...stateR1c.verdicts.values()];
  const n = (k) => v.filter((x) => x.verdict === k).length;
  console.log("\nCross-component dependency invalidation (condition-insensitive: an edge is may-reach)");
  console.log("  what the unfollowed dependencies were");
  for (const [k, c] of Object.entries(stateR1c.kinds)) console.log("    " + k.padEnd(16) + c);
  console.log("  prop edges built");
  console.log("    from the value          " + stateR1c.stats.value);
  console.log("    from identity, rebuilt every render  " + stateR1c.stats.identity);
  console.log("    forwarded actual->formal " + stateR1c.stats.forwarded);
  console.log("    identity not readable    " + stateR1c.stats.unresolvedProp);
  console.log("    a name in it this pass cannot place  " + stateR1c.stats.unplaceable);
  console.log("    prop the child never destructures    " + stateR1c.stats.notDestructured);
  console.log("  state cells, dependency axis with props");
  for (const k of ["MUTATION", "UNRESOLVED", "READ_ONLY"]) console.log("    " + k.padEnd(12) + n(k));

  const merged = new Map();
  for (const [k, x] of stateR1c.verdicts) merged.set(k, { dep: x.verdict, life: "READ_ONLY", cell: x.cell, why: x.why });
  for (const [k, x] of stateR1b.verdicts) if (merged.has(k)) { merged.get(k).life = x.verdict; merged.get(k).lifeWhy = x.why; }
  const worst = (m) => (m.dep === "MUTATION" || m.life === "MUTATION" ? "MUTATION" : m.dep === "UNRESOLVED" || m.life === "UNRESOLVED" ? "UNRESOLVED" : "READ_ONLY");
  const t = { MUTATION: 0, UNRESOLVED: 0, READ_ONLY: 0 };
  for (const m of merged.values()) t[worst(m)]++;
  console.log("\n  all three axes together");
  for (const k of ["MUTATION", "UNRESOLVED", "READ_ONLY"]) console.log("    " + k.padEnd(12) + t[k]);

  const suspended = roots.filter((r) => r.verdict === "READ_ONLY" && r.stateWrites.length);
  const tally = { MUTATION: 0, UNRESOLVED: 0, READ_ONLY: 0, UNMAPPED: 0 };
  for (const r of suspended) {
    const seen = [];
    for (const w of r.stateWrites) {
      const keys = stateR1.cellsForWrite(w);
      if (!keys) { seen.push("UNMAPPED"); continue; }
      for (const k of keys) seen.push(merged.has(k) ? worst(merged.get(k)) : "UNMAPPED");
    }
    tally[seen.includes("MUTATION") ? "MUTATION" : seen.includes("UNMAPPED") ? "UNMAPPED"
      : seen.includes("UNRESOLVED") ? "UNRESOLVED" : "READ_ONLY"]++;
  }
  console.log("\n  suspended roots, all three axes");
  for (const k of ["MUTATION", "UNRESOLVED", "READ_ONLY", "UNMAPPED"]) console.log("    " + k.padEnd(12) + tally[k]);

  if (process.env.PROBE === "cross") {
    console.log("");
    for (const x of [...stateR1c.verdicts.values()].sort((a, b) => (a.cell.path + a.cell.line).localeCompare(b.cell.path + b.cell.line))) {
      console.log("  " + x.verdict.padEnd(11) + x.cell.path.replace("src/ui/", "") + ":" + x.cell.line + "  " +
        x.cell.comp + "." + x.cell.value + "  effects=" + x.effects + "  " + x.why.replace(/src\/ui\//g, ""));
    }
  }
}

{
  const n = (v, from = stateR2) => from.filter((x) => x.verdict === v).length;
  const suspended = stateR2.filter((x) => x.root.verdict === "READ_ONLY" && x.root.stateWrites.length);
  console.log("\nInteraction root verdicts (join: direct, dependency, identity, lifecycle)");
  for (const v of ["MUTATION", "UNRESOLVED", "READ_ONLY"]) console.log("  " + v.padEnd(12) + String(n(v)).padStart(4) + "   of " + stateR2.length);
  const fam = new Map();
  for (const x of stateR2) if (x.verdict === "MUTATION") fam.set(x.family ?? "unstated", (fam.get(x.family ?? "unstated") ?? 0) + 1);
  console.log("\n  MUTATION by causal family");
  for (const [k, v] of [...fam.entries()].sort((a, b) => b[1] - a[1])) console.log("    " + k.padEnd(22) + v);
  const why = new Map();
  for (const x of stateR2) if (x.verdict === "UNRESOLVED") for (const o of x.open) why.set(o, (why.get(o) ?? 0) + 1);
  console.log("\n  UNRESOLVED by open edge (a root can carry more than one)");
  for (const [k, v] of [...why.entries()].sort((a, b) => b[1] - a[1]).slice(0, 12)) console.log("    " + String(v).padStart(4) + "  " + k);
  console.log("\n  the roots that were suspended on their state writes   " + suspended.length);
  for (const v of ["MUTATION", "UNRESOLVED", "READ_ONLY"]) console.log("    " + v.padEnd(12) + n(v, suspended));
  console.log("\n  MUTATION verdicts carrying no witness   " + mutNoWitness + "   (must be 0)");

  // The total root ledger. The sampled reports above are for reading; a
  // migration that must not move a semantic byte needs every root's verdict,
  // witness and open edges, not the first eight of each kind.
  if (process.env.PROBE === "roots") {
    console.log("");
    for (const x of [...stateR2].sort((a, b) => (a.root.path + ":" + String(a.root.line).padStart(6, "0"))
        .localeCompare(b.root.path + ":" + String(b.root.line).padStart(6, "0")))) {
      const at = x.root.path + ":" + x.root.line;
      console.log("  " + at + "  " + x.verdict);
      console.log("      action    " + (x.root.actions?.length ? [...x.root.actions].sort().join(" ") : "-"));
      console.log("      kind      " + (x.root.kind ?? "-"));
      console.log("      axes      direct=" + x.axes.direct + " dependency=" + x.axes.dependency +
        " identity=" + x.axes.identity + " lifecycle=" + x.axes.lifecycle);
      console.log("      witness   " + (x.witness ?? "-"));
      console.log("      family    " + (x.family ?? "-"));
      console.log("      cells     " + (x.cells.length ? [...new Set(x.cells)].sort().join(" ") : "-"));
      console.log("      open      " + (x.open.length ? [...x.open].sort().join(" | ") : "-"));
      console.log("      causes    " + (x.causes.length
        ? x.causes.map((c) => c.source).sort().join(" ").replace(/src\/ui\//g, "") : "-"));
      for (const o of [...(x.root.open ?? [])].sort((a, b) => (a.kind + a.at).localeCompare(b.kind + b.at))) {
        console.log("        direct  " + o.kind + "  " + o.at);
      }
    }
  }
  if (process.env.PROBE === "r2") {
    console.log("");
    for (const x of stateR2.filter((r) => r.root.verdict === "READ_ONLY" && r.root.stateWrites.length)
        .sort((a, b) => (a.root.path + a.root.line).localeCompare(b.root.path + b.root.line))) {
      const at = x.root.path.replace("src/ui/", "") + ":" + x.root.line;
      if (x.verdict === "MUTATION") console.log("  MUTATION   " + at + "  [" + x.family + "]  " + (x.witness ?? "").replace(/src\/ui\//g, ""));
      else if (x.verdict === "UNRESOLVED") console.log("  UNRESOLVED " + at + "  " + x.open.join(" | ").replace(/src\/ui\//g, ""));
      else console.log("  READ_ONLY  " + at + "  direct=none  writes=" + x.cells.map((c) => c.split("#").slice(1).join(".")).join(",") +
        "  dependency=none  identity=none  lifecycle=none  open=none");
    }
  }
}

// A root verdict of UNRESOLVED is a fact inside the denominator. A source this
// walk never placed is a hole in the denominator itself, and the two must not
// be reported as one number. The seal is not "N is 413" — a new button moves N
// and that is a product change, not a regression. It is that every production
// registration of a user input is either a certified root or a stated
// non-user judgement, and that every certified root carries exactly one
// effect verdict.
{
  const dom = roots.filter((r) => r.kind === "dom-event").length;
  const jsx = roots.filter((r) => r.kind === "jsx-handler").length;
  const chord = roots.filter((r) => r.kind === "command-chord").length;
  const un = uncertified.length;
  console.log("\nInteraction source universe");
  console.log("  certified roots                  " + roots.length);
  console.log("    JSX handler attributes         " + jsx);
  console.log("    DOM listeners                  " + dom);
  console.log("    shortcut table entries         " + chord);
  console.log("  stated non-user registrations    " + nonUser.length);
  for (const [w, c] of Object.entries(nonUser.reduce((m, u) => ({ ...m, [u.why]: (m[u.why] ?? 0) + 1 }), {}))) {
    console.log("    " + w.padEnd(30) + c);
  }
  // One certification, three readers. What it proved and what it refused is
  // printed where the denominator is, because a refusal is what keeps a call
  // spelled like a registration from being treated as one.
  const rp = new Map();
  for (const f of registrationFacts) {
    const k = f.operation + "  " + (f.refusal ?? "proven:" + f.targetProvenance);
    rp.set(k, (rp.get(k) ?? 0) + 1);
  }
  console.log("  event registrations              " + registrationFacts.length);
  for (const [k, v] of [...rp.entries()].sort()) console.log("    " + k.padEnd(32) + v);
  console.log("  refused declarations             " + refused.length +
    "   (must be 0: an action on a non-input, or one an element cannot place)");
  for (const d of refused) console.log("    " + d.why + "  " + d.path.replace("src/ui/", "") + ":" + d.line + "  " + (d.detail ?? ""));
  console.log("  UNCERTIFIED sources              " + un + "   (must be 0 to seal the denominator)");
  for (const u of uncertified) {
    console.log("    " + u.why + "  " + u.path.replace("src/ui/", "") + ":" + u.line);
  }
  console.log("\n  certified roots with no effect verdict   " + noVerdict + "   (must be 0)");
  console.log("  MUTATION with no positive witness        " + mutNoWitness + "   (must be 0)");
  console.log("  READ_ONLY with a reachable open edge     " + roNotClosed + "   (must be 0)");
  console.log("\n  denominator " + (denominatorSealed
    ? "SEALED at " + roots.length + " for this revision" : "NOT SEALED"));
}

// Forward closure says every certified root gets a verdict. The reverse asks
// the other question: does every mutation this product can perform have a
// stated origin? Not "every sink must have a root" — startup, reconciliation
// and background maintenance write too, and forcing those into a UI action
// would be the same conflation in the opposite direction. Four answers, and
// only the last one is a hole.
{
  const mutatingCaps = [...capabilities].filter((c) => capabilityMutates(c));
  const byRoot = new Set();
  for (const r of roots) for (const m of r.mutates) byRoot.add(m);
  const byEffect = new Set();
  for (const e of effectRows) {
    for (const m of [...e.setup.direct, ...e.setup.scheduled]) byEffect.add(m);
    if (e.cleanup && !e.cleanup.unresolved) for (const m of [...e.cleanup.direct, ...e.cleanup.scheduled]) byEffect.add(m);
  }
  const origin = new Map();
  for (const c of mutatingCaps) {
    origin.set(c, byRoot.has(c) ? "USER_INTERACTION" : byEffect.has(c) ? "HOST_LIFECYCLE"
      : productFiles.has(capabilityMutates(c).split("#")[0]) ? "UNRESOLVED" : "OFFSTAGE");
  }
  const tally = (v) => [...origin.values()].filter((x) => x === v).length;
  console.log("\nMutation capabilities, by where they are driven from");
  for (const k of ["USER_INTERACTION", "HOST_LIFECYCLE", "OFFSTAGE", "UNRESOLVED"]) {
    console.log("  " + k.padEnd(20) + String(tally(k)).padStart(4) + (k === "UNRESOLVED" ? "   (must be 0 to close the reverse direction)" : ""));
  }
  if (process.env.PROBE === "origin") {
    for (const [c, v] of [...origin.entries()].sort()) console.log("    " + v.padEnd(18) + c);
  }
}

{
  const n = (k) => stateActions.rows.filter((x) => x.state === k).length;
  const A = stateActions.actions;
  console.log("\nAction membership (identity is the product's; the analyzer never invents one)");
  console.log("  certified roots                  " + roots.length);
  console.log("    NAMED, declared at the source  " + n("NAMED"));
  console.log("    NAMED by forwarding            " + stateActions.forwarded + "   (a second authority, with no source material in this tree)");
  console.log("    UNDECLARED                     " + n("UNDECLARED") + "   (the product names no action here; not a proof that none exists)");
  console.log("  declared actions                 " + A.length);
  console.log("    driven by more than one root   " + A.filter((a) => a.roots.length > 1).length);
  console.log("    by more than one input kind    " + A.filter((a) => a.modalities.size > 1).length + "   (modality independence)");
  console.log("    by roots in more than one file " + A.filter((a) => a.files.size > 1).length + "   (location independence)");
  console.log("\n  action effect, joined from its roots (a root verdict is never rewritten)");
  for (const k of ["MUTATION", "UNRESOLVED", "READ_ONLY"]) console.log("    " + k.padEnd(12) + A.filter((a) => a.effect === k).length);
  const mixed = A.filter((a) => a.mixed);
  console.log("\n  actions whose roots do not agree  " + mixed.length + "   (no role taxonomy invented for them)");
  for (const a of mixed) {
    console.log("    " + a.id.padEnd(26) + a.effect.padEnd(11) + a.roots.map((r, i) => r.path.replace("src/ui/", "") + ":" + r.line + "=" + a.verdicts[i]).join("  "));
  }
  if (REGISTRY) {
    const declared = new Set(A.map((a) => a.id));
    const missing = [...REGISTRY.keys()].filter((id) => !declared.has(id));
    const extra = [...declared].filter((id) => !REGISTRY.has(id));
    console.log("\n  registry <-> production roots");
    console.log("    registry entries               " + REGISTRY.size);
    console.log("    with no production root        " + missing.length + (missing.length ? "   " + missing.join(", ") : ""));
    console.log("    rendered but not in registry   " + extra.length + (extra.length ? "   " + extra.join(", ") : ""));
    // The registry declares identity and contract. What an action does is its
    // roots' answer, and a kind that disagrees is the registry claiming an
    // effect for itself — the self-certification this whole exercise removes.
    // Only the kinds that make an effect claim can disagree with one. `view`
    // says it must not reach the kernel and `navigation` says it changes no
    // session state; `kernel-mutation` and `destructive` say they do. The rest
    // — repeatable, interaction, shell-native — describe a contract along
    // another axis entirely, and reading them as effect claims manufactured
    // four disagreements that were only a wrong mapping.
    const claimsClean = new Set(["view", "navigation"]);
    const claimsWrite = new Set(["kernel-mutation", "destructive"]);
    const disagree = A.filter((a) => {
      const k = REGISTRY.get(a.id);
      if (!k || a.effect === "UNRESOLVED") return false;
      return (claimsClean.has(k) && a.effect === "MUTATION") || (claimsWrite.has(k) && a.effect === "READ_ONLY");
    });
    console.log("    a kind that claims an effect, against what its roots do   " + disagree.length);
    for (const a of disagree) console.log("      " + a.id.padEnd(26) + "registry=" + REGISTRY.get(a.id).padEnd(16) + "roots=" + a.effect);
  }
  if (process.env.PROBE === "actions") {
    console.log("");
    for (const a of A.sort((x, y) => x.id.localeCompare(y.id))) {
      console.log("  " + a.effect.padEnd(11) + a.id.padEnd(28) + [...a.modalities].sort().join("+").padEnd(22) + "  " +
        a.roots.map((r, i) => r.path.replace("src/ui/", "") + ":" + r.line + "=" + a.verdicts[i]).join("  "));
    }
  }
}

// 59 named and 354 undeclared is not the number that matters. This is: how many
// inputs already proven to write the host have no action identity at all, and
// how many that might yet be proven to write have none either. Those two cells
// are what a Tier denominator waits on; a read-only undeclared root delays only
// the vocabulary, not the tier.
{
  const cell = (m, v) => stateR2.filter((x) => membership.get(x.root) === m && x.verdict === v);
  console.log("\nMembership against effect");
  console.log("                    NAMED   UNDECLARED");
  for (const v of ["MUTATION", "UNRESOLVED", "READ_ONLY"]) {
    console.log("  " + v.padEnd(18) + String(cell("NAMED", v).length).padStart(5) + String(cell("UNDECLARED", v).length).padStart(12));
  }
  const blockers = [...cell("UNDECLARED", "MUTATION"), ...cell("UNDECLARED", "UNRESOLVED")];
  console.log("\n  inputs with no action identity that a Tier denominator waits on   " + blockers.length);
  console.log("    proven to write                " + cell("UNDECLARED", "MUTATION").length);
  console.log("    might yet be proven to write   " + cell("UNDECLARED", "UNRESOLVED").length);
  console.log("    read-only, vocabulary only     " + cell("UNDECLARED", "READ_ONLY").length);
  const bySource = (rs) => Object.entries(rs.reduce((m, x) => ({ ...m, [x.root.kind]: (m[x.root.kind] ?? 0) + 1 }), {}))
    .map(([k, v]) => k + "=" + v).join("  ");
  console.log("\n  by source kind");
  console.log("    proven to write                " + bySource(cell("UNDECLARED", "MUTATION")));
  console.log("    might yet be proven to write   " + bySource(cell("UNDECLARED", "UNRESOLVED")));
  // A JSX element takes data-action today. A DOM listener and a shortcut have
  // no attribute to carry one; the shortcut table already declares its own, and
  // a listener has nowhere to. That gap is the product's to close, and until it
  // does these inputs cannot be given an identity by anything here.
  const domBlocked = blockers.filter((x) => x.root.kind === "dom-event");
  console.log("\n  of those, DOM listeners not declared through listenAction   " + domBlocked.length);
  for (const x of domBlocked) {
    console.log("    " + x.verdict.padEnd(11) + x.root.path.replace("src/ui/", "") + ":" + x.root.line + "  " + x.root.event + " on " + (x.root.receiver ?? "?"));
  }

  const A = stateActions.actions;
  const tier = (a) => (a.effect === "MUTATION" ? "PROVEN_TIER_A" : a.effect === "UNRESOLVED" ? "POTENTIAL_TIER_A" : "PROVEN_NON_MUTATING");
  console.log("\nDeclared actions, by what their roots prove");
  for (const k of ["PROVEN_TIER_A", "POTENTIAL_TIER_A", "PROVEN_NON_MUTATING"]) {
    console.log("  " + k.padEnd(22) + A.filter((a) => tier(a) === k).length);
  }
  console.log("  — a count over the " + A.length + " ids the product declares, not over its actions:");
  console.log("    " + blockers.length + " inputs that can or might write are still outside the action universe.");

  if (process.env.PROBE === "unnamed") {
    for (const v of ["MUTATION", "UNRESOLVED"]) {
      console.log("\n  UNDECLARED + " + v);
      for (const x of cell("UNDECLARED", v).sort((a, b) => (a.root.path + a.root.line).localeCompare(b.root.path + b.root.line))) {
        console.log("    " + x.root.path.replace("src/ui/", "") + ":" + x.root.line + "  " + x.root.kind.replace("jsx-handler", "jsx") +
          "/" + (x.root.event ?? "") + "  " + (x.root.label || "").slice(0, 28) + (x.family ? "  [" + x.family + "]" : ""));
      }
    }
  }
}

{
  const state = new Map(stateActions.rows.map((x) => [x.root, x.state]));
  const A = stateActions.actions;
  // A member root's verdict is read from R2 and never written back, so an
  // action can be mutating while the click that opens it is not. Checked rather
  // than asserted: a mixed action must still show every verdict it joined.
  const rule = (ok, name, detail) => console.log("  " + (ok === null ? "n/a   " : ok ? "held  " : "OPEN  ") + name.padEnd(60) + (detail ?? ""));
  console.log("\nSeal conditions for the action universe");
  rule(mutUnnamed === 0, "A  every proven-writing input has an identity", mutUnnamed + " without one");
  rule(unrUnnamed === 0, "B  every input that might write has one", unrUnnamed + " without one");
  rule(true, "C  membership is never inferred from a shared handler", "literal declarations only");
  rule(REGISTRY ? orphans === 0 : null, "D  every registry action has a production root", REGISTRY ? orphans + " orphaned" : "no registry in this tree");
  rule(REGISTRY ? strays === 0 : null, "E  every rendered action id is in the registry", REGISTRY ? strays + " missing" : "no registry in this tree");
  rule(true, "F  an action's effect is joined from its roots alone", "registry kind never participates");
  rule(true, "G  identity survives a change of input kind and of place", "held by fixtures V and W");
  rule(mixedKept, "H  a mixed action keeps each root's own verdict", "held by fixture X and mcp.remove");
  console.log("\n  Tier universe " + (mutUnnamed === 0 && unrUnnamed === 0 ? "can be frozen" : "NOT frozen: A and B are what it waits on"));
}

// A review artifact, not a fact. The tool can enumerate what needs a name and
// lay out the evidence a person needs to give one — where it is, what it does,
// what it is labelled, what its component already declares — and it stops
// there. It does not propose an id: an action is what a person thinks they are
// doing, and the only mechanical source for that would be the label's wording
// or the port method behind it, which are the two inferences this whole census
// exists to refuse. Naming stays with whoever owns the vocabulary; what lands
// in the source is the canonical declaration.
if (process.env.PROBE === "candidates" || process.env.CLUSTER) {
  const state = new Map(stateActions.rows.map((x) => [x.root, x.state]));
  const declaredIn = new Map();
  for (const r of roots) for (const id of r.actions ?? []) {
    const k = r.path + "#" + (r.comp ?? "?");
    if (!declaredIn.has(k)) declaredIn.set(k, new Set());
    declaredIn.get(k).add(id);
  }
  const rank = { MUTATION: 0, UNRESOLVED: 1, READ_ONLY: 2 };
  const only = process.env.CLUSTER;
  const rows = stateR2.filter((x) => state.get(x.root) !== "NAMED")
    .filter((x) => !only || (x.root.path.includes(only) && x.verdict === "MUTATION"))
    .sort((a, b) => rank[a.verdict] - rank[b.verdict] ||
      (a.root.path + String(a.root.line).padStart(5, "0")).localeCompare(b.root.path + String(b.root.line).padStart(5, "0")));
  console.log(["verdict", "where", "source", "on screen", "aria/title", "data-target", "data-value",
    "ids nearby", "class", "capability", "witness"].join("\t"));
  for (const x of rows) {
    const r = x.root;
    const near = [...(declaredIn.get(r.path + "#" + (r.comp ?? "?")) ?? [])].sort();
    console.log([
      x.verdict,
      r.path.replace("src/ui/", "") + ":" + r.line,
      r.kind.replace("jsx-handler", "jsx").replace("command-chord", "chord") + (r.event ? "/" + r.event : ""),
      r.text || "", (r.label || "").replace(/\s+/g, " ").slice(0, 40),
      r.dataTarget ?? "", r.dataValue ?? "",
      near.join(" ") || "-",
      x.verdict === "UNRESOLVED" ? "AMBIGUOUS" : near.length ? "REUSE?" : "NEW?",
      (r.mutates ?? []).map((m) => String(m).split("#").pop()).join(" "),
      (x.witness ?? "").replace(/src\/ui\//g, "").slice(0, 90),
    ].join("\t"));
  }
}


if (process.env.PROBE === "transport" || process.env.PROBE === "endpoints") {
  const t = transport;
  const keyOf = (s) => s.verb + " " + s.path;
  const bucket = conservation;
  const old = conservation.old;
  console.log("\nTransport-R1 (endpoint ownership; verdicts are built on this)");
  console.log("  transport edges (endpoint is the caller's)   " + t.edges.size);
  for (const [k, e] of t.edges) console.log("    " + e.verb.padEnd(7) + k.replace("src/port/", "") + "  param " + e.param + "  " + e.via);
  console.log("  endpoint sites through an edge               " + t.sites.length);
  console.log("  endpoint sites inline (unchanged)            " + t.inlineSites.filter((s) => s.path !== null).length);
  console.log("  dynamic endpoints, unresolved                " + t.dynamic.length);
  const keys = [...new Set([...t.sites, ...t.inlineSites.filter((s) => s.path)].map(keyOf))].sort();
  console.log("  distinct EndpointKey(verb, path)             " + keys.length);
  console.log("\n  endpoint adjudication");
  console.log("    declared non-mutating         " + keys.filter((k) => k in NOT_A_MUTATION).length);
  console.log("    mutating                      " + keys.filter((k) => !(k in NOT_A_MUTATION)).length);
  for (const k of keys.filter((k) => k in NOT_A_MUTATION)) console.log("      NON-MUTATING  " + k);
  if (SRC !== "src") for (const k of keys.filter((k) => !(k in NOT_A_MUTATION))) console.log("      mutating      " + k);
  if (process.env.PROBE === "endpoints") {
    console.log("\n  every endpoint site");
    const rows = [...t.sites.map((x) => ({ ...x, via: "edge" })),
      ...t.inlineSites.map((x) => ({ ...x, via: "inline" }))]
      .sort((a, b) => (a.at + ":" + String(a.line).padStart(6, "0")).localeCompare(b.at + ":" + String(b.line).padStart(6, "0")));
    for (const x of rows) {
      console.log("    " + (x.at + ":" + x.line).padEnd(44) + x.via.padEnd(7) +
        (x.verb ?? "?").padEnd(7) + (x.path ?? "<not readable>").padEnd(34) + x.owner);
    }
    console.log("\n  every EndpointKey, adjudicated");
    for (const k of keys) {
      const d = NOT_A_MUTATION[k];
      console.log("    " + (d ? "NON-MUTATING" : "mutating    ") + "  " + k.padEnd(34) + (d ? d.class : "-"));
    }
    console.log("\n  every dynamic endpoint");
    for (const d of [...t.dynamic].sort((a, b) => (a.at + a.line).localeCompare(b.at + b.line))) {
      console.log("    " + d.at + ":" + d.line + "  " + d.verb + "  " + d.why + "  " + d.owner);
    }
  }
  console.log("\n  conservation over the old mutating capabilities   " + old.length);
  console.log("    reach an endpoint site        " + bucket.site.length);
  console.log("    reach only a dynamic endpoint " + bucket.dynamic.length);
  console.log("    reach only an unreadable fetch " + bucket.unreadable.length);
  console.log("    silently lost                 " + bucket.lost.length + "   (must be 0)");
  if (bucket.lost.length) console.log("      " + bucket.lost.join(" "));
  if (bucket.dynamic.length) console.log("      dynamic: " + bucket.dynamic.join(" "));
}


// Seal-B0. No verdict moves here and nothing is named: it decomposes the causes
// behind the roots that might write and have no action identity, so the next
// cut is chosen by payoff rather than by which reason prints most often.
if (process.env.PROBE === "b0") reportUnresolvedCauses();


// Seal-B1e-0. Shadow only: the fact set is built and printed, and no verdict,
// witness or open edge reads it. What a continuation runs is B1e-1, and
// splitting them is what makes a moved verdict attributable to one of the two.
if (process.env.PROBE === "promises") {
  const rows = promiseSites.filter((s) => productFiles.has(s.path));
  const proven = rows.filter((s) => s.on);
  const tally = (list, of) => {
    const m = new Map();
    for (const x of list) m.set(of(x), (m.get(of(x)) ?? 0) + 1);
    return [...m].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
  };
  const pad = (s) => (s + "                                   ").slice(0, 35);
  console.log("\nPromise value provenance (shadow: nothing reads this yet)");
  console.log("  continuation calls written        " + rows.length);
  console.log("  receiver proven a Promise         " + proven.length);
  for (const [k, n] of tally(proven, (s) => s.on.provenance)) console.log("    " + pad(k) + n);
  console.log("    " + pad("deepest proof, receivers deep") + promiseDepth);
  console.log("  receiver unproven                 " + (rows.length - proven.length));
  for (const [k, n] of tally(rows.filter((s) => !s.on), (s) => s.shape)) console.log("    " + pad(k) + n);
  console.log("\n  proven continuations by member");
  for (const [k, n] of tally(proven, (s) => "." + s.member)) console.log("    " + pad(k) + n);
  const cbs = proven.flatMap((s) => s.callbacks);
  console.log("\n  callbacks they hand out           " + cbs.length);
  for (const [k, n] of tally(cbs, (c) => (/^(Arrow)?Function(Expression|Declaration)$/.test(c.arg.type) ? "function literal"
    : c.arg.type === "Identifier" ? "a name" : c.arg.type))) console.log("    " + pad(k) + n);
  console.log("\n  every receiver this pass cannot prove");
  for (const s of rows.filter((x) => !x.on).sort((a, b) => (a.path + a.line).localeCompare(b.path + b.line))) {
    console.log("    " + pad(s.shape) + s.path + ":" + s.line + "  " + (s.comp ?? "?") + "  ." + s.member);
  }
}
