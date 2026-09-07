// State-R1b: lifecycle presence reachability — a write that mounts or unmounts
// a component runs that component's effects.
import { stateR1 } from "./state_dependency.mjs";
import { presenceCauses } from "./causes.mjs";
import { alias, bodyOf as aliasOf, memoized } from "./component_identity.mjs";
import { baseNamesOf, declaredName, ownerOf, resolveCallee } from "./symbols.mjs";
import { external, flat, importsOf, isCall, jsxName, productFiles, trees, walk, walkStack } from "./source.mjs";
import { callsOf } from "./capabilities.mjs";
import { sites } from "./roots.mjs";
import { effectRows } from "./effects.mjs";
import { mutating } from "./sinks.mjs";

// A dependency change is one of three ways React runs an effect. The other two
// are the component arriving and the component leaving, and a state write can
// cause either by deciding whether the element is rendered at all — or by
// changing its key, which is a new instance rather than the same one updated.
//
// What this must not do is read an ancestor's rerender as a mount. A component
// rendered unconditionally is the same instance across its parent's every
// render, so its `[]` setup runs once no matter how much state the parent
// writes; fixture I is that control, and without it the one mounting mutation
// in this tree would turn every ancestor's state into a writer.
const stateR1b = (() => {
  const { cells, memos, byComp, edges, at, resolveBinding } = stateR1;
  const isFn = (x) => x.type === "ArrowFunctionExpression" || x.type === "FunctionExpression" || x.type === "FunctionDeclaration";
  const fnName = declaredName;
  // Which cells can invalidate a memo, so a guard written against a memoised
  // value carries the state behind it.
  const cellsReaching = new Map();
  for (const c of cells.values()) {
    const seen = new Set([c.key]);
    const queue = [{ node: c.key, unresolved: false }];
    while (queue.length) {
      const cur = queue.pop();
      for (const e of edges.get(cur.node) ?? []) {
        if (e.to.startsWith("effect:")) continue;
        const unresolved = cur.unresolved || e.unresolved;
        const mark = e.to + (unresolved ? "!" : "");
        if (seen.has(mark)) continue;
        seen.add(mark);
        if (!cellsReaching.has(e.to)) cellsReaching.set(e.to, []);
        cellsReaching.get(e.to).push({ cell: c.key, unresolved });
        queue.push({ node: e.to, unresolved });
      }
    }
  }

  // `const Pane = memo(PaneView)` renders one name and holds another's body, so
  // a site for the wrapper is a site for what it wraps. Without this the whole
  // subtree under the wrapper reports its presence as unreadable, which reads
  // as caution and is really a broken edge.
  // Who calls whom, so a hook that owns effects can be placed at the components
  // that call it: a hook is never rendered, and its lifecycle is theirs.
  const callers = new Map();
  for (const [key, called] of callsOf) {
    const file = key.slice(0, key.lastIndexOf("#"));
    for (const nm of called) for (const target of resolveCallee(file, nm)) {
      if (!callers.has(target)) callers.set(target, new Set());
      callers.get(target).add(key);
    }
  }

  // Every render site, with what decides whether it is there.
  const sites = new Map();
  const dynamicTargets = [];
  for (const [path, tree] of trees) {
    if (!productFiles.has(path)) continue;
    walkStack(tree, (n, stack) => {
      if (n.type !== "JSXOpeningElement") return;
      const name = jsxName(n.name);
      if (!/^[A-Z]/.test(name)) return;
      const imp = importsOf.get(path)?.get(name);
      const decl = imp?.file ?? path;
      const declName = imp?.name ?? name;
      const id = decl + "#" + declName;
      const host = ownerOf(stack);
      const guards = [];
      let opaque = null;
      for (let i = stack.length - 1; i > 0; i--) {
        const child = stack[i], parent = stack[i - 1];
        if (parent.type === "LogicalExpression" && parent.right === child) { guards.push(parent.left); continue; }
        if (parent.type === "ConditionalExpression" && parent.test !== child) { guards.push(parent.test); continue; }
        if (parent.type === "IfStatement" && parent.test !== child) { guards.push(parent.test); continue; }
        if (!isFn(child)) continue;
        // A function boundary. The component's own body ends the walk; a list
        // callback runs per element, so the list decides presence; anything
        // else is a callback whose invocation this pass cannot place.
        if (fnName(child, parent) === host) break;
        const callee = isCall(parent) ? flat(parent.callee) : "";
        if (/\.(map|flatMap)$/.test(callee)) { guards.push(parent.callee.object ?? parent.callee); continue; }
        opaque = "rendered inside " + (callee ? callee + "(...)" : parent.type);
        break;
      }
      const keyAttr = n.attributes.find((a) => a.type === "JSXAttribute" && a.name?.name === "key" && a.value);
      const key = keyAttr ? (keyAttr.value.type === "JSXExpressionContainer" ? keyAttr.value.expression : keyAttr.value) : null;
      const props = new Map();
      for (const a of n.attributes) {
        if (a.type !== "JSXAttribute" || !a.value || a.name?.name === "key") continue;
        props.set(a.name.name, a.value.type === "JSXExpressionContainer" ? a.value.expression : a.value);
      }
      const target = aliasOf(id);
      if (!sites.has(target)) sites.set(target, []);
      sites.get(target).push({ file: path, line: n.loc.start.line, host, guards, key, opaque, props, unconditional: !guards.length && !opaque });
      // A target whose declaration is not a function body is a component chosen
      // at runtime; nothing here can say what decides its presence.
      const declTree = trees.get(decl);
      if (!declTree || external.get(path)?.has(name)) return;
      if (alias.has(id)) return;
      let isComponent = false;
      walk(declTree, (d) => {
        if ((d.type === "FunctionDeclaration" || d.type === "ClassDeclaration") && d.id?.name === declName) isComponent = true;
        if (d.type === "VariableDeclarator" && d.id?.type === "Identifier" && d.id.name === declName) {
          const init = isCall(d.init) ? d.init.arguments[0] : d.init;
          if (init && isFn(init)) isComponent = true;
        }
      });
      if (!isComponent) dynamicTargets.push(id + "  at " + path + ":" + n.loc.start.line);
    });
  }

  const outer = [];
  const opaquePresence = [];
  const nameSources = (path, comp, nm, into, why) => {
    const r = resolveBinding(path, comp, nm);
    for (const c of r.cells) into.set(c, why);
    for (const m of r.memos) for (const x of cellsReaching.get(m) ?? []) into.set(x.cell, why + (x.unresolved ? "+unresolved" : ""));
    for (const o of r.outer) outer.push({ site: path + "#" + comp, name: o, why });
    // Nothing left to read: every cell here may be behind it.
    if (r.opaque.length) byComp.get(path + "#" + (comp ?? "?"))?.cells.forEach((c) => into.set(c, why + "+opaque-local"));
  };

  const byOwner = new Map();
  for (const e of effectRows) {
    const id = e.path + "#" + (e.comp ?? "?");
    if (!byOwner.has(id)) byOwner.set(id, []);
    byOwner.get(id).push(e);
  }
  const presenceCache = new Map();
  const presenceSources = (id, chain) => {
    if (chain.has(id)) return { from: new Map(), opaque: [] };
    if (presenceCache.has(id)) return presenceCache.get(id);
    chain.add(id);
    const from = new Map(), opaque = [];
    const list = sites.get(id) ?? [];
    // Never rendered: if something calls it, it is a hook and its lifecycle is
    // its callers'. If nothing does either, its presence is genuinely unknown.
    if (!list.length) {
      const who = [...(callers.get(id) ?? [])].filter((k) => byOwner.has(k) || sites.has(k));
      if (!who.length) opaque.push(id + ": never rendered and never called in the product graph");
      for (const c of who) {
        const up = presenceSources(c, chain);
        for (const [k, why] of up.from) from.set(k, from.get(k) ?? "caller:" + why);
        opaque.push(...up.opaque);
      }
    }
    for (const s of list) {
      if (s.opaque) opaque.push(s.file + ":" + s.line + " " + s.opaque);
      for (const g of s.guards) for (const nm of baseNamesOf(g)) nameSources(s.file, s.host, nm, from, "guard");
      // A new key is a new instance: the old one unmounts and a new one mounts.
      if (s.key) for (const nm of baseNamesOf(s.key)) nameSources(s.file, s.host, nm, from, "key");
      // Whatever brings the host into existence brings this with it. An
      // unconditional site adds nothing of its own — that is the whole of what
      // separates a rerender from a mount.
      if (s.host) {
        const up = presenceSources(s.file + "#" + s.host, chain);
        for (const [k, why] of up.from) from.set(k, from.get(k) ?? "host:" + why);
        opaque.push(...up.opaque);
      }
    }
    chain.delete(id);
    const out = { from, opaque };
    presenceCache.set(id, out);
    return out;
  };

  // What a component's arrival and departure would run.
  const danger = [];
  for (const [id, list] of byOwner) {
    const mounts = list.some((e) => e.setup.direct.length || e.setup.scheduled.length);
    const unmounts = list.some((e) => e.cleanup && !e.cleanup.unresolved && (e.cleanup.direct.length || e.cleanup.scheduled.length));
    const mountOpen = list.some((e) => e.setup.open > 0);
    const unmountOpen = list.some((e) => e.cleanup?.unresolved || (e.cleanup && e.cleanup.open > 0));
    // Which effect carries it, so a verdict can name the thing it rests on.
    const at = (f) => { const e = list.find(f); return e ? "effect:" + e.path + ":" + e.line : null; };
    if (mounts || unmounts || mountOpen || unmountOpen) {
      danger.push({ id, mounts, unmounts, mountOpen, unmountOpen,
        by: at((e) => e.setup.direct.length || e.setup.scheduled.length) ??
            at((e) => e.cleanup && !e.cleanup.unresolved && (e.cleanup.direct.length || e.cleanup.scheduled.length)) ??
            at((e) => e.setup.open > 0) ?? at((e) => e.cleanup?.unresolved || (e.cleanup && e.cleanup.open > 0)) });
    }
  }

  const verdicts = new Map();
  // A cell can gate several components, and each is its own reason. The first
  // one met used to be the only one written down, which made the order the
  // danger set happens to be built in the owner of what a cell's debt is.
  const note = (cellKey, verdict, why, effect, family, causes) => {
    const prev = verdicts.get(cellKey);
    const openCauses = [...(prev?.openCauses ?? []), ...(causes ?? [])];
    if (prev && (prev.verdict === "MUTATION" || verdict !== "MUTATION")) {
      if (prev) prev.openCauses = openCauses;
      return;
    }
    verdicts.set(cellKey, { verdict, why, effect, family, openCauses });
  };
  for (const d of danger) {
    const p = presenceSources(d.id, new Set());
    if (p.opaque.length) opaquePresence.push({ id: d.id, why: p.opaque.slice(0, 3) });
    for (const [cellKey, why] of p.from) {
      const mutating = d.mounts || d.unmounts;
      const verdict = mutating && !why.includes("unresolved") ? "MUTATION" : "UNRESOLVED";
      note(cellKey, verdict,
        d.id + (d.mounts ? " mount-mutation" : d.unmounts ? " unmount-mutation" : d.mountOpen ? " mount-open" : " unmount-open") + " via " + why,
        d.by, d.mounts ? "mount" : d.unmounts ? "unmount" : "lifecycle-open",
        verdict === "UNRESOLVED" ? presenceCauses(d, stateR1.effects, why) : []);
    }
  }
  return { verdicts, danger, sites, dynamicTargets, opaquePresence, outer, byOwner, memoized, cellsReaching };
})();

export { stateR1b };
