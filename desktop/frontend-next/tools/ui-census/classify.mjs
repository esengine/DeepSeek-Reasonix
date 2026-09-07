// What a handler expression reaches. Every verdict in this census is built on
// this walk, and nothing it cannot follow is reported as clean.
import { callMode, flat, importsOf, isCall, reactImports, trees, walk } from "./source.mjs";
import { HOPS, PLATFORM, localsOf, renders } from "./sites.mjs";
import { resolveCallee } from "./symbols.mjs";
import { callsParam, params } from "./params.mjs";
import { callsOf, capabilities } from "./capabilities.mjs";
import { capabilityMutates, receiverKind } from "./types.mjs";
import { mutating, unresolvedTransport } from "./sinks.mjs";
import { byNode as registrations } from "./registrations.mjs";
import { byCall as eventParamCalls } from "./event_params.mjs";
import { byNode as schedulers } from "./schedulers.mjs";
import { EDGE, byNode as continuations } from "./promises.mjs";
import { effectivePropSource } from "./prop_sources.mjs";

// Every trail element that means the work does not run in the execution that
// reached it. One vocabulary: the continuation labels come from the fact that
// builds them, so a role added there cannot be forgotten here.
const DEFERRED = new RegExp("^(Scheduled|" + Object.values(EDGE).join("|") + ")\\(");
const deferred = (trail) => trail.some((t) => DEFERRED.test(t));

// Not by the spelling setX: the provenance is an import of useState from react
// and the second slot of the array it destructures. What this proves is narrow
// — the callee is a state update, and React runs the updater it is given — and
// not that the interaction is read-only, which depends on whether any reactive
// effect projects that state onto the kernel.
const GLOBAL_CALLABLE = new Set(["setTimeout", "setInterval", "queueMicrotask", "requestAnimationFrame",
  "requestIdleCallback", "addEventListener", "removeEventListener", "fetch", "structuredClone"]);

const stateSetters = new Set();
for (const [path, tree] of trees) {
  const localUseState = new Set();
  walk(tree, (n) => {
    if (n.type !== "ImportDeclaration" || n.source.value !== "react") return;
    for (const sp of n.specifiers) {
      if (sp.type === "ImportSpecifier" && sp.imported.name === "useState") localUseState.add(sp.local.name);
    }
  });
  walk(tree, (n) => {
    if (n.type !== "VariableDeclarator" || n.id?.type !== "ArrayPattern" || !isCall(n.init)) return;
    const callee = flat(n.init.callee);
    if (!localUseState.has(callee) && callee !== "React.useState") return;
    const slot = n.id.elements[1];
    if (slot?.type === "Identifier") stateSetters.add(path + "#" + slot.name);
  });
}

// The four callbacks whose contract this tree can prove, and what each promises
// about the function it is handed. Keyed by API identity — a name imported from
// react, a global binding — never by member spelling.
//
// useCallback does not run the callback. What it returns is the callable this
// hook callsite memoises, and its body comes from the argument; it is not the
// same object across renders, and nothing here claims it is.
const CALLBACK_CONTRACT = {
  "React.useCallback": "callable-proxy",
  "React.useMemo": "may-call-sync",
  "globalThis.setTimeout": "schedule",
  "globalThis.setInterval": "schedule",
};
const contractOf = (path, callee) => {
  const rl = reactImports.get(path);
  if (rl?.has(callee)) return CALLBACK_CONTRACT["React." + rl.get(callee)] ?? null;
  if (!callee.includes(".")) return CALLBACK_CONTRACT["globalThis." + callee] ?? null;
  return null;
};

// Every open edge carries the site it was found at, not only the trail that
// reached it. Two roots reaching one unreadable call are one thing to fix, and
// a trail differs per root — counting trails counted paths as causes.
function classify(expr, path, comp, depth, seen, out, trail, skip) {
  if (!expr) return;
  // Resolving what a binding is, rather than what runs now: a useCallback here
  // is the callable this binding holds, so its body is what a caller reaches.
  if (isCall(expr) && contractOf(path, flat(expr.callee)) === "callable-proxy") {
    const arg = expr.arguments[0];
    if (arg) classify(arg, path, comp, depth + 1, seen, out, [...trail, "CallableProxy(" + path + ":" + expr.loc.start.line + ")"], skip);
    return;
  }
  if (skip?.has(expr)) return;
  if (depth > HOPS) { out.open.push({ kind: "hop-limit", at: trail.join(" -> "), site: path + "#" + (comp ?? "?") + "#" + depth }); return; }
  if (!trees.has(path)) { out.open.push({ kind: "file-outside-scan", at: path, site: path }); return; }
  const locals = localsOf(path);

  // How each name was reached, not only that it was. A call that may be skipped
  // still may happen, so an optional one is a mutation edge like any other —
  // but the witness has to say which, because "reaches" and "always runs" are
  // different claims and the second was never being made.
  const names = new Map();
  const reach = (name, node) => {
    const mode = node ? callMode(node) : "REQUIRED";
    const at = node?.loc ? path + ":" + node.loc.start.line : null;
    const prev = names.get(name);
    if (!prev || (prev.mode === "OPTIONAL" && mode === "REQUIRED")) names.set(name, { mode, at });
  };
  if (expr.type === "Identifier" || expr.type === "MemberExpression") reach(flat(expr), null);
  // Defining a function is not calling it. Walking the whole subtree credited
  // an effect with everything its nested listener would do when a person later
  // pressed a key: code living inside a scope is not code this execution root
  // runs. A nested body is reached only through a CALL edge.
  const isFn = (x) => x.type === "ArrowFunctionExpression" || x.type === "FunctionExpression" || x.type === "FunctionDeclaration";
  const executed = (node, fn) => {
    if (!node || typeof node.type !== "string") return;
    fn(node);
    for (const [k, v] of Object.entries(node)) {
      if (k === "loc" || k.endsWith("Comments")) continue;
      const visit = (c) => { if (c && typeof c.type === "string" && !isFn(c)) executed(c, fn); };
      if (Array.isArray(v)) v.forEach(visit);
      else visit(v);
    }
  };
  const argBodies = [];
  executed(isFn(expr) ? expr.body : expr, (n) => {
    if (!isCall(n)) return;
    // Installing or removing a listener is the platform's operation, not a name
    // this walk failed to place, and the listener it is handed is reached by
    // the event rather than by whoever registered it. Read from the certified
    // fact — matching the callee's spelling here would be a second authority,
    // and a parameter named addEventListener would win the platform's identity.
    if (registrations.has(n)) return;
    // A method on the event this handler was handed. Proven only where the
    // receiver is the certified parameter itself: `e.currentTarget.blur()` is a
    // different receiver and stays unresolved, and so does a member no contract
    // covers — a proven receiver does not prove what is reachable through it.
    const ev = eventParamCalls.get(n);
    if (ev) {
      if (ev.kind) {
        out.open.push({ kind: ev.kind, at: trail.join(" -> ") + " :: " + flat(n.callee),
          site: path + "#" + (comp ?? "?") + "#" + flat(n.callee), member: ev.member, event: ev.fact.eventKind });
      }
      return;
    }
    // A continuation on a value proven to be a Promise. Which member was
    // written decides nothing: the receiver's proof is what earns the edge, and
    // the fact refuses every receiver it cannot trace to a declared return
    // type. What is earned is a deferred edge, because a handler that mutates
    // when the request comes back is not one that mutates now.
    const cont = continuations.get(n);
    if (cont) {
      out.scheduled.add(cont.edgeAt);
      // Where the optionality is, the same as everywhere else: a chain entered
      // through `p?.then(cb)` reaches whatever cb reaches, and the evidence has
      // to say the continuation may be skipped at the hop that may skip it.
      const via = callMode(n) === "OPTIONAL" ? ["MayCall(" + cont.edgeAt + ")"] : [];
      for (const cb of cont.callbacks) {
        // The two shapes a callback is written in. Anything else is a value
        // this pass cannot open, and saying so is not the same as saying the
        // continuation runs nothing.
        if (isFn(cb.arg) || cb.arg.type === "Identifier") argBodies.push({ arg: cb.arg, node: cb.edge + "(" + cont.edgeAt + ")", via });
        else out.open.push({ kind: "promise-callback-unresolved", at: trail.join(" -> ") + " :: ." + cont.member + "[" + cb.index + "]",
          site: path + "#" + (comp ?? "?") + "#." + cont.member + "[" + cb.index + "]" });
      }
      return;
    }
    if (n.callee.type === "MemberExpression" && n.callee.computed) out.open.push({ kind: "computed-dispatch", at: trail.join(" -> "),
      site: path + "#" + (comp ?? "?") + "#" + n.loc.start.line });
    reach(flat(n.callee), n);
    // A function handed to a call runs only if that position is called. Which
    // position, decided by the callee's own body — not by the argument looking
    // like a function.
    // A state setter runs the updater it is handed. The call itself is a local
    // state update and reaches no host; whatever the updater does still counts.
    // A contract decides what this call does with the function it is given.
    const contract = contractOf(path, flat(n.callee));
    if (contract) {
      const arg = n.arguments[0];
      const at = path + ":" + n.loc.start.line;
      // Memoising does not run it. The callable is reached through whatever
      // holds the result, which is a different root's question.
      if (contract === "callable-proxy") return;
      if (contract === "may-call-sync" && arg) argBodies.push({ arg, node: "MayCallSync(" + at + ")" });
      // Scheduled work runs, but not in this execution. The edge is recorded so
      // an effect that only schedules is not reported as one that mutates.
      if (contract === "schedule" && arg) { out.scheduled.add(at); argBodies.push({ arg, node: "Scheduled(" + at + ")" }); }
      return;
    }
    if (stateSetters.has(path + "#" + flat(n.callee))) {
      // Recorded, not discarded: a verdict of READ_ONLY that rests on a state
      // update is only as good as the effect closure over that state, which
      // nothing has checked yet.
      out.stateWrites.add(path + "#" + (comp ?? "?") + "#" + flat(n.callee));
      names.delete(flat(n.callee));
      n.arguments.forEach((arg) => {
        if (isFn(arg)) argBodies.push({ arg, node: "ReactUpdater(" + path + ":" + n.loc.start.line + ")" });
      });
      return;
    }
    n.arguments.forEach((arg, i) => {
      if (!isFn(arg)) return;
      const keys = resolveCallee(path, flat(n.callee));
      const known = keys.find((k) => params.has(k));
      if (!known) {
        const callee = flat(n.callee);
        const member = callee.includes(".") ? callee.split(".").pop() : callee;
        // How strongly the callee's identity is known, which decides whether a
        // contract may ever be written for it. A name imported from react is an
        // identity; "then" on an unknown object is a spelling.
        const rl = reactImports.get(path);
        const provenance =
          rl?.has(callee) ? "import-proven:React." + rl.get(callee)
          : !callee.includes(".") && GLOBAL_CALLABLE.has(callee) ? "global-proven:globalThis." + callee
          : /^(window|globalThis|document)\./.test(callee) ? "receiver-proven:" + callee
          : callee.includes(".") ? "member-name-only:." + member
          : "unbound-name:" + callee;
        // Whether this call is a platform scheduler, read from the fact that
        // proves it is one rather than from the callee's spelling. Recorded, not
        // acted on: what a scheduler contract would be worth is measured before
        // it is written.
        out.open.push({ kind: "callee-unresolved", callee, provenance, at: trail.join(" -> ") + " :: " + callee,
          site: path + "#" + (comp ?? "?") + "#" + callee, scheduler: schedulers.get(n)?.name ?? null });
        return;
      }
      if (callsParam(known, i)) argBodies.push({ arg, node: "ActualArg(" + path + ":" + n.loc.start.line + "," + i + ")" });
      else out.open.push({ kind: "callback-not-proven-executed", at: trail.join(" -> ") + " :: " + flat(n.callee) + "[" + i + "]",
        site: path + "#" + (comp ?? "?") + "#" + flat(n.callee) + "[" + i + "]" });
    });
  });
  for (const { arg, node, via } of argBodies) {
    if (trail.includes(node)) continue;
    classify(arg, path, comp, depth + 1, seen, out, [...trail, ...(via ?? []), node], skip);
  }

  for (const [name, how] of names) {
    const root = name.split(".")[0];
    // A trail element, so a verdict built on a call that may not run says so.
    // Where the optionality is, not where the trail ends: a chain entered
    // through `onAction?.(…)` reaches a write, and the evidence has to say the
    // call may be skipped at the hop that may skip it.
    const via = how.mode === "OPTIONAL" ? ["MayCall(" + (how.at ?? path) + ")"] : [];
    const edge = (target) => (how.mode === "OPTIONAL" ? "MayCall(" + (how.at ?? path) + "," + target + ")" : target);
    if (PLATFORM.test(root)) continue;
    // A capability reached through the port interface.
    if (name.includes(".")) {
      const member = name.split(".").pop();
      const kind = capabilities.has(member) ? receiverKind(path, name.slice(0, name.lastIndexOf("."))) : "OTHER";
      if (kind === "UNPROVEN") {
        out.open.push({ kind: "capability-receiver-unproven", provenance: "receiver:" + name.slice(0, name.lastIndexOf(".")),
          site: path + "#" + (comp ?? "?") + "#" + name,
          at: trail.join(" -> ") + " :: " + name });
        continue;
      }
      if (kind === "PORT") {
        const hit = capabilityMutates(member);
        if (hit) {
          out.mutates.add(member);
          // Which causal edge carried it. A mutation reached only through a
          // scheduled callback is not something this execution performs.
          (deferred(trail) ? out.scheduledMutations : out.directMutations).add(member);
          out.trailOf ??= [...trail, edge(member)].join(" -> ");
          continue;
        }
        continue;
      }
    }
    // Anything the mutation fixpoint already decided.
    const keys = resolveCallee(path, name);
    if (keys.some((k) => unresolvedTransport.has(k))) {
      out.open.push({ kind: "transport-endpoint-unresolved", at: trail.join(" -> ") + " :: " + name,
        site: path + "#" + (comp ?? "?") + "#" + name });
      continue;
    }
    const decided = keys.find((k) => mutating.has(k));
    if (decided) {
      out.mutates.add(decided);
      // The same causal edge the capability branch records. Leaving it off here
      // meant a mutation reached through an ordinary function was counted in
      // the verdict and absent from every count of how effects mutate.
      (deferred(trail) ? out.scheduledMutations : out.directMutations).add(decided);
      out.trailOf ??= [...trail, edge(decided)].join(" -> ");
      continue;
    }
    if (locals.has(root)) {
      const node = "Local(" + path + "," + root + ")";
      // A binding handed to addEventListener is reached by the event, not by
      // whoever installed it. Excluding the argument node is not enough: what
      // is registered is the name, and the body lives at its declaration.
      if (skip?.has(node)) continue;
      if (trail.includes(node)) continue;
      if (seen.has(node)) continue;
      seen.add(node);
      classify(locals.get(root), path, comp, depth + 1, seen, out, [...trail, ...via, node], skip);
      continue;
    }
    if (/^on[A-Z]/.test(root)) {
      // The prop as the component declares it. Distinct from every expression a
      // parent passes for it: three bindings spelled onPick are three nodes, and
      // collapsing them turned ordinary forwarding into a cycle.
      const formal = "Formal(" + path + "#" + (comp ?? "?") + "," + root + ")";
      if (!comp) { out.open.push({ kind: "enclosing-component-unknown", at: trail.join(" -> ") + " :: " + root,
        site: path + "#?#" + root }); continue; }
      // Revisiting a node already on the path is a fixpoint, not an unknown:
      // whatever it reaches is being computed by the traversal already inside
      // it. Cycles were only ever a symptom of node identity collapsing.
      if (trail.includes(formal)) continue;
      let found = false;
      for (const site of renders.get(path + "#" + comp) ?? []) {
        // Which attribute provides this prop is JSX's answer, not a map's: a
        // spread to the right of an explicit attribute replaces it, and one
        // whose contents cannot be read may.
        const eff = effectivePropSource(site, root);
        if (eff.status === "ABSENT") continue;
        found = true;
        if (eff.status === "UNRESOLVED") {
          out.open.push({ kind: "prop-spread-source-unresolved", at: trail.join(" -> ") + " :: " + root,
            site: site.file + "#" + (site.host ?? "?") + "#" + root });
          continue;
        }
        const actual = "Actual(" + site.file + ":" + site.line + "," + root + ")";
        // Which production binding this root was wired through. Two roots that
        // meet at the same actual argument are wired to the same thing; that is
        // evidence they may be one intent, and never a reason to merge them.
        out.actuals?.add(actual);
        if (trail.includes(actual)) continue;
        if (seen.has(actual)) continue;
        seen.add(actual);
        for (const src of eff.sources) {
          const inner = src.type === "JSXExpressionContainer" ? src.expression : src;
          classify(inner, site.file, site.host, depth + 1, seen, out, [...trail, ...via, formal, actual], skip);
        }
      }
      if (!found) {
        // Why no actual was found, which is four different debts wearing one
        // name: the scope is a helper rather than a component, the component
        // body is rendered under another identity, a spread carries the prop
        // set, or the prop is genuinely never passed.
        const at = renders.get(path + "#" + comp) ?? [];
        const shape = !at.length
          ? (/^[A-Z]/.test(comp ?? "") ? "component-not-a-render-target" : "scope-is-not-a-component")
          : at.some((r) => r.spreads.length) ? "spread-carries-it"
          : "prop-never-passed";
        out.open.push({ kind: "prop-source", at: trail.join(" -> ") + " :: " + root,
          site: path + "#" + (comp ?? "?") + "#" + root, shape });
      }
      continue;
    }
    if (stateSetters.has(path + "#" + root)) { out.stateWrites.add(path + "#" + (comp ?? "?") + "#" + root); continue; }
    if (/^(set[A-Z]|use[A-Z])/.test(root)) continue;
    const imported = importsOf.get(path)?.get(root);
    if (imported) {
      // Imported and not mutating by the fixpoint: it has been decided.
      if (callsOf.has(imported.file + "#" + imported.name) || trees.has(imported.file)) continue;
      out.open.push({ kind: "import-unresolved", at: trail.join(" -> ") + " :: " + root,
        site: path + "#" + (comp ?? "?") + "#" + root });
      continue;
    }
    if (!/^[a-z]/.test(root)) continue;
    // The base name is what could not be placed; the whole callee and where it
    // was written are what a later pass needs to say why. `baseUrl.trim` and
    // `baseUrl` reach here identically and are not the same question.
    out.open.push({ kind: "unknown-callee", at: trail.join(" -> ") + " :: " + root,
      site: path + "#" + (comp ?? "?") + "#" + name,
      name, receiver: name.includes(".") ? name.slice(0, name.lastIndexOf(".")) : null, path, comp });
  }
}

export { CALLBACK_CONTRACT, GLOBAL_CALLABLE, classify, contractOf, stateSetters };
