// State-R1: what a write to a state cell can retrigger, along the dependency axis.
import { external, flat, importsOf, isCall, productFiles, reactImports, trees, walk, walkStack } from "./source.mjs";
import { baseNamesOf, ownerOf } from "./symbols.mjs";
import { formals, localInits, paramInits } from "./flow.mjs";
import { effectRows } from "./effects.mjs";
import { primaryReason, reachCauses } from "./causes.mjs";

// React reruns an effect when a dependency's identity changes, and a memo hook
// is where a state change becomes a new identity. The edge to follow is that
// invalidation — state -> memo -> ... -> effect — not whether the effect's
// array happens to spell the state's name. Both directions are held by the
// fixture: F reaches its effect through a memo that lists the state, and G is
// the same shape with an empty memo list and must reach nothing.
//
// A dependency change runs the old cleanup and then the new setup, so both
// lifecycles propagate here. Unmount is a different trigger and is not this
// relation's: a `[]` effect's cleanup stays out until an unmount reachability
// pass exists for it.
const stateR1 = (() => {
  const hookOf = (path, callee) => {
    const rl = reactImports.get(path);
    if (rl?.has(callee)) return rl.get(callee);
    const m = /^React\.(\w+)$/.exec(callee);
    return m ? m[1] : null;
  };
  const depsOf = (call) => {
    const d = call.arguments[1];
    if (!d) return { kind: "EVERY_COMMIT", names: [] };
    if (d.type !== "ArrayExpression") return { kind: "UNRESOLVED_DEPS", names: [] };
    if (d.elements.length === 0) return { kind: "MOUNT_ONLY", names: [] };
    const names = d.elements.map((e) => (e ? flat(e) : "?"));
    return { kind: names.includes("?") ? "UNRESOLVED_DEPS" : "EXPLICIT_DEPS", names };
  };

  const cells = new Map();
  const memos = new Map();
  const bySetter = new Map();
  const inFile = new Map();
  const byComp = new Map();
  const unreadableCells = [];
  const at = (path, comp, name) => path + "#" + (comp ?? "?") + "#" + name;
  for (const [path, tree] of trees) {
    if (!productFiles.has(path)) continue;
    walkStack(tree, (n, stack) => {
      if (n.type !== "VariableDeclarator") return;
      const owner = path + "#" + (ownerOf(stack) ?? "?");
      if (!byComp.has(owner)) byComp.set(owner, { cells: [], memos: [], locals: new Set() });
      walk(n.id, (b) => { if (b.type === "Identifier") byComp.get(owner).locals.add(b.name); });
      if (!isCall(n.init)) return;
      const hook = hookOf(path, flat(n.init.callee));
      if (!/^(useState|useReducer|useCallback|useMemo)$/.test(hook ?? "")) return;
      const comp = ownerOf(stack) ?? "?";
      const scope = path + "#" + comp;
      if (!byComp.has(scope)) byComp.set(scope, { cells: [], memos: [], locals: new Set() });
      if (hook === "useState" || hook === "useReducer") {
        // A state cell is the pair the hook returns. Destructured any other way
        // it is not readable here, and saying so is the answer.
        if (n.id.type !== "ArrayPattern" || n.id.elements[0]?.type !== "Identifier") {
          unreadableCells.push(path + ":" + n.loc.start.line);
          return;
        }
        const value = n.id.elements[0].name;
        const setter = n.id.elements[1]?.type === "Identifier" ? n.id.elements[1].name : null;
        const key = at(path, comp, value);
        cells.set(key, { key, path, comp, value, setter, line: n.loc.start.line });
        byComp.get(scope).cells.push(key);
        if (setter) {
          bySetter.set(path + "#" + comp + "#" + setter, [key]);
          const wide = path + "#" + setter;
          inFile.set(wide, inFile.has(wide) ? [...inFile.get(wide), key] : [key]);
        }
        return;
      }
      if (n.id.type !== "Identifier") return;
      const key = at(path, comp, n.id.name);
      memos.set(key, { key, path, comp, name: n.id.name, deps: depsOf(n.init), line: n.loc.start.line });
      byComp.get(scope).memos.push(key);
    });
  }

  // A local that is neither state nor memo is not opaque merely for not being a
  // hook: `const activePort = panePorts.get(active)` states its own sources in
  // its initialiser. Reading that is structure, not inference — refusing to
  // read it is what turns one derived name into every cell in the component.
  const resolveBinding = (path, comp, name, out, seen, depth) => {
    out ??= { cells: new Set(), memos: new Set(), outer: [], opaque: [] };
    seen ??= new Set();
    depth ??= 0;
    const k = at(path, comp, name);
    if (cells.has(k)) { out.cells.add(k); return out; }
    if (memos.has(k)) { out.memos.add(k); return out; }
    const bound = paramInits.get(k);
    if (bound && !seen.has(k) && depth < 8) {
      seen.add(k);
      if (bound.kind === "state") {
        for (const c of byComp.get(path + "#" + (comp ?? "?"))?.cells ?? []) {
          if (cells.get(c)?.setter === bound.setter) out.cells.add(c);
        }
        return out;
      }
      for (const b of baseNamesOf(bound.node)) resolveBinding(path, comp, b, out, seen, depth + 1);
      return out;
    }
    if (!byComp.get(path + "#" + (comp ?? "?"))?.locals.has(name)) {
      // A prop or a parameter is somebody else's actual — R1c's edge. A module
      // binding or an import is nobody's state. A name that is neither is one
      // this pass cannot place, and "not connected" would be the wrong answer.
      if (formals.get(path + "#" + (comp ?? "?"))?.has(name)) out.outer.push(name);
      else if (importsOf.get(path)?.has(name) || external.get(path)?.has(name) ||
               byComp.get(path + "#?")?.locals.has(name) || /^[A-Z_]/.test(name) || name === "undefined") out.outer.push(name);
      else out.opaque.push(name);
      return out;
    }
    const init = localInits.get(k);
    const names = init && !seen.has(k) && depth < 8 ? baseNamesOf(init) : null;
    if (!names?.size) { out.opaque.push(name); return out; }
    seen.add(k);
    for (const b of names) resolveBinding(path, comp, b, out, seen, depth + 1);
    return out;
  };

  // Which nodes invalidate a consumer with this dependency list, and what the
  // list leaves unanswered. An outer name — a prop, a module binding — is a
  // real edge from somewhere else's state and is counted rather than dropped.
  const sourcesFor = (path, comp, deps) => {
    const scope = byComp.get(path + "#" + (comp ?? "?")) ?? { cells: [], memos: [], locals: new Set() };
    const none = { proven: [], maybe: [], outer: [] };
    if (deps.kind === "MOUNT_ONLY") return { ...none, why: "mount-only" };
    if (deps.kind === "EVERY_COMMIT") return { ...none, proven: scope.cells, why: "every-commit" };
    if (deps.kind === "UNRESOLVED_DEPS") {
      return { ...none, maybe: [...scope.cells, ...scope.memos], why: "unresolved-deps" };
    }
    const proven = [], outer = [];
    let opaque = false;
    for (const nm of deps.names) {
      // `a.b` changes only when a does, so the base is the invalidation source.
      // React compares the member, so this may retrigger more often than it
      // does — the safe direction for a question about what a write can reach.
      const r = resolveBinding(path, comp, nm.split(".")[0]);
      proven.push(...r.cells, ...r.memos);
      outer.push(...r.outer);
      if (r.opaque.length) opaque = true;
    }
    return { proven, maybe: opaque ? scope.cells : [], outer, why: "explicit" };
  };

  const edges = new Map();
  const link = (from, to, unresolved, why) => {
    if (!edges.has(from)) edges.set(from, []);
    edges.get(from).push({ to, unresolved, why });
  };
  const outerDeps = [];
  const wire = (r, to, site, owner) => {
    for (const f of r.proven) link(f, to, false, r.why);
    for (const f of r.maybe) link(f, to, true, r.why);
    for (const o of r.outer) outerDeps.push({ site, owner, name: o });
  };
  for (const m of memos.values()) wire(sourcesFor(m.path, m.comp, m.deps), m.key, "memo " + m.path + ":" + m.line, m.path + "#" + m.comp);
  const effects = new Map();
  for (const e of effectRows) {
    const key = "effect:" + e.path + ":" + e.line;
    effects.set(key, e);
    wire(sourcesFor(e.path, e.comp, e.deps), key, "effect " + e.path + ":" + e.line, e.path + "#" + (e.comp ?? "?"));
  }

  // What one write reaches, and whether anything on the way was unreadable.
  const reach = (cellKey) => {
    const hit = new Map();
    // The dependency lists on this cell's reach that could not be read. The
    // cell's own, so they are collected once rather than per effect.
    const opaque = new Set();
    const seen = new Set([cellKey]);
    const queue = [{ node: cellKey, unresolved: false }];
    while (queue.length) {
      const cur = queue.pop();
      for (const e of edges.get(cur.node) ?? []) {
        const unresolved = cur.unresolved || e.unresolved;
        if (e.unresolved) opaque.add(e.why + "@" + e.to);
        if (effects.has(e.to)) {
          if (!hit.has(e.to) || (hit.get(e.to) && !unresolved)) hit.set(e.to, unresolved);
          continue;
        }
        const mark = e.to + (unresolved ? "!" : "");
        if (seen.has(mark)) continue;
        seen.add(mark);
        queue.push({ node: e.to, unresolved });
      }
    }
    return { hit, opaque };
  };

  const verdicts = new Map();
  for (const c of cells.values()) {
    const { hit, opaque } = reach(c.key);
    let mutation = null;
    for (const [ek, edgeUnresolved] of hit) {
      const e = effects.get(ek);
      const setupMut = e.setup.direct.length || e.setup.scheduled.length;
      const cleanMut = e.cleanup && !e.cleanup.unresolved && (e.cleanup.direct.length || e.cleanup.scheduled.length);
      if ((setupMut || cleanMut) && !edgeUnresolved) mutation ??= ek;
    }
    const openCauses = reachCauses(hit, effects, opaque);
    const unresolved = primaryReason(openCauses);
    verdicts.set(c.key, {
      cell: c, effects: hit.size,
      verdict: mutation ? "MUTATION" : openCauses.length ? "UNRESOLVED" : "READ_ONLY", openCauses,
      witness: mutation,
      why: mutation ?? unresolved ?? (hit.size ? "reaches only effects that neither mutate nor leave anything open" : "reaches no effect"),
    });
  }
  // Which cells a recorded write names. A component-scoped hit is the answer;
  // a file-wide one is only an answer when the name is unique in that file,
  // because seven components spelling their setter setX are seven cells and
  // collapsing them credits every one of them with the worst of the seven.
  const cellsForWrite = (w) => {
    if (bySetter.has(w)) return bySetter.get(w);
    const parts = w.split("#");
    const wide = inFile.get(parts[0] + "#" + parts[parts.length - 1]);
    return wide && wide.length === 1 ? wide : null;
  };
  return { cells, memos, effects, verdicts, cellsForWrite, outerDeps, unreadableCells, byComp, edges, at, resolveBinding };
})();

export { stateR1 };
