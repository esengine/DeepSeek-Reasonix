// State-R1c: cross-component dependency invalidation, along prop edges.
import { stateR1 } from "./state_dependency.mjs";
import { primaryReason, reachCauses } from "./causes.mjs";
import { sites } from "./roots.mjs";
import { stateR1b } from "./state_lifecycle.mjs";
import { external, flat, importsOf, isCall, productFiles, trees, walk, walkStack } from "./source.mjs";
import { formals, positional } from "./flow.mjs";
import { effectRows } from "./effects.mjs";
import { baseNamesOf, ownerOf, resolveCallee } from "./symbols.mjs";

// React reruns an effect when Object.is says a dependency changed, and there
// are two ways a parent's state does that to a child's prop. One is the value:
// the prop is computed from the state. The other has nothing to do with the
// value — an object literal, an array, an inline arrow is built again on every
// render of the parent, so a write to any of the parent's state hands the child
// a new identity and the effect reruns with the same contents. Reading only the
// first answers "this prop does not depend on that state" about a prop that
// reruns the effect every single time it is written.
//
// Both are condition-insensitive: a guard inside an effect or a branch that
// never runs is not modelled, so an edge here means may-reach, not will-reach.
const stateR1c = (() => {
  const { cells, memos, byComp, edges, at, resolveBinding } = stateR1;
  const { sites, memoized, cellsReaching } = stateR1b;


  // Where an expression's value ends up coming from, following only the
  // positions that can be its result.
  const results = (e, fn) => {
    if (!e) return;
    if (e.type === "TSAsExpression" || e.type === "TSNonNullExpression" || e.type === "TSTypeAssertion" || e.type === "ParenthesizedExpression") return results(e.expression, fn);
    if (e.type === "ConditionalExpression") { results(e.consequent, fn); results(e.alternate, fn); return; }
    if (e.type === "LogicalExpression") { results(e.left, fn); results(e.right, fn); return; }
    if (e.type === "SequenceExpression") return results(e.expressions[e.expressions.length - 1], fn);
    fn(e);
  };
  // Built again on every evaluation, so its identity is new whether or not its
  // contents changed.
  const FRESH = new Set(["ObjectExpression", "ArrayExpression", "ArrowFunctionExpression", "FunctionExpression",
    "JSXElement", "JSXFragment", "NewExpression", "TaggedTemplateExpression", "ClassExpression"]);
  // Compared by value, or a binding whose identity is somebody else's fact.
  const BY_VALUE = new Set(["StringLiteral", "NumericLiteral", "BooleanLiteral", "NullLiteral", "BigIntLiteral",
    "Identifier", "MemberExpression", "OptionalMemberExpression", "TemplateLiteral", "BinaryExpression",
    "UnaryExpression", "UpdateExpression", "JSXText", "RegExpLiteral"]);
  // A call's identity is unknown in general, but not when the source states the
  // return type: a function declared to return a string returns something
  // Object.is compares by value, so it cannot be the reason an effect reruns.
  // 95 of the unreadable props in this tree were one such function.
  const PRIMITIVE = /^TS(String|Number|Boolean|BigInt|Symbol|Void|Undefined|Null)Keyword$/;
  const primitiveReturn = (t) => {
    if (!t) return false;
    const a = t.type === "TSTypeAnnotation" ? t.typeAnnotation : t;
    if (PRIMITIVE.test(a.type) || a.type === "TSLiteralType") return true;
    if (a.type === "TSUnionType") return a.types.every((x) => PRIMITIVE.test(x.type) || x.type === "TSLiteralType");
    return false;
  };
  const returnsByValue = new Map();
  const calleeReturnsPrimitive = (path, callee) => {
    const name = callee.split(".")[0];
    if (callee.includes(".")) return false;
    const imp = importsOf.get(path)?.get(name);
    const file = imp?.file ?? path;
    const want = imp?.name ?? name;
    const memo = file + "#" + want;
    if (returnsByValue.has(memo)) return returnsByValue.get(memo);
    let hit = false;
    const t = trees.get(file);
    if (t) walk(t, (d) => {
      if (d.type === "FunctionDeclaration" && d.id?.name === want) hit ||= primitiveReturn(d.returnType);
      if (d.type === "VariableDeclarator" && d.id?.type === "Identifier" && d.id.name === want &&
          (d.init?.type === "ArrowFunctionExpression" || d.init?.type === "FunctionExpression")) hit ||= primitiveReturn(d.init.returnType);
    });
    returnsByValue.set(memo, hit);
    return hit;
  };
  const shapeOf = (e, path) => {
    let recreated = false, unresolved = false;
    results(e, (x) => {
      if (FRESH.has(x.type)) recreated = true;
      else if (BY_VALUE.has(x.type)) return;
      else if (isCall(x) && path && calleeReturnsPrimitive(path, flat(x.callee))) return;
      else unresolved = true;
    });
    return { recreated, unresolved };
  };

  // Which cells can cause a component to render again. Its own, and its host's
  // — unless a memo() stands between them and every prop crossing it is stable,
  // which is the only thing that stops a parent's render from reaching down.
  const rerenderCache = new Map();
  const rerenders = (id, chain) => {
    if (chain.has(id)) return { set: new Set(), exact: false };
    if (rerenderCache.has(id)) return { set: rerenderCache.get(id), exact: true };
    chain.add(id);
    const out = new Set(byComp.get(id)?.cells ?? []);
    let exact = true;
    for (const s of sites.get(id) ?? []) {
      const stops = memoized.has(id) && ![...s.props.values()].some((e) => { const sh = shapeOf(e, s.file); return sh.recreated || sh.unresolved; });
      if (stops) continue;
      const up = rerenders(s.file + "#" + s.host, chain);
      exact &&= up.exact;
      for (const c of up.set) out.add(c);
    }
    chain.delete(id);
    if (exact) rerenderCache.set(id, out);
    return { set: out, exact };
  };

  const extra = new Map();
  const link = (from, to, unresolved, why) => {
    if (!extra.has(from)) extra.set(from, []);
    extra.get(from).push({ to, unresolved, why });
  };
  const propKey = (id, name) => "prop:" + id + "#" + name;

  // A dependency that is a formal prop is fed by every site that renders it.
  const consumers = [];
  const wireDeps = (id, deps, node) => {
    if (deps.kind !== "EXPLICIT_DEPS") return;
    const f = formals.get(id);
    if (!f) return;
    for (const nm of deps.names) {
      const base = nm.split(".")[0];
      if (!f.has(base)) continue;
      link(propKey(id, f.get(base)), node, false, "formal");
      consumers.push({ id, prop: f.get(base), node });
    }
  };
  for (const e of effectRows) wireDeps(e.path + "#" + (e.comp ?? "?"), e.deps, "effect:" + e.path + ":" + e.line);
  for (const m of memos.values()) wireDeps(m.path + "#" + m.comp, m.deps, m.key);

  const stats = { value: 0, identity: 0, forwarded: 0, moduleBinding: 0, unresolvedProp: 0, unplaceable: 0, notDestructured: 0 };
  // One edge builder for both kinds of actual: what the value comes from, and
  // whether the expression hands over a new identity on every render.
  const feed = (key, expr, file, host, hostId) => {
    const r = { cells: new Set(), memos: new Set(), outer: [], opaque: [] };
    for (const b of baseNamesOf(expr)) resolveBinding(file, host, b, r);
    for (const c of r.cells) { link(c, key, false, "value"); stats.value++; }
    for (const m of r.memos) { link(m, key, false, "value"); stats.value++; }
    for (const o of r.outer) {
      const hf = formals.get(hostId);
      // The host's own incoming binding, forwarded on: one more actual/formal pair.
      if (hf?.has(o)) { link(propKey(hostId, hf.get(o)), key, false, "forwarded"); stats.forwarded++; }
      else stats.moduleBinding++;
    }
    const sh = shapeOf(expr, file);
    // An expression already rebuilt on every render hands over a new identity
    // whatever its contents are, and that edge is exact. Adding an unresolved
    // one from the same cells for a name inside it only makes a known answer
    // look uncertain.
    if (r.opaque.length && !sh.recreated) {
      for (const c of rerenders(hostId, new Set()).set) link(c, key, true, "opaque-local");
      stats.unplaceable++;
    }
    if (sh.recreated || sh.unresolved) {
      for (const c of rerenders(hostId, new Set()).set) link(c, key, !sh.recreated, sh.recreated ? "identity" : "identity-unresolved");
      if (sh.recreated) stats.identity++; else stats.unresolvedProp++;
    }
  };
  for (const [id, list] of sites) {
    const f = formals.get(id);
    for (const s of list) {
      const hostId = s.file + "#" + s.host;
      for (const [name, expr] of s.props) {
        // A child that does not destructure this prop reads it some other way;
        // nothing here follows `props.x`.
        if (!f?.has(name) && ![...(f?.values() ?? [])].includes(name)) { stats.notDestructured++; continue; }
        feed(propKey(id, name), expr, s.file, s.host, hostId);
      }
    }
  }
  // A hook takes its inputs as arguments rather than as JSX attributes, and the
  // effects inside it are the caller's lifecycle. Same actual/formal pair, same
  // two ways a caller's state moves it: the value, and a fresh identity.
  for (const [path, tree] of trees) {
    if (!productFiles.has(path)) continue;
    walkStack(tree, (n, stack) => {
      if (!isCall(n) || !n.arguments.length) return;
      const host = ownerOf(stack);
      for (const target of resolveCallee(path, flat(n.callee))) {
        if (!formals.has(target)) continue;
        n.arguments.forEach((a, i) => {
          const name = positional.get(target + "@" + i);
          if (!name || a.type === "SpreadElement") return;
          feed(propKey(target, "@" + i), a, path, host, path + "#" + host);
        });
        break;
      }
    });
  }

  // The whole graph: local dependencies, memo invalidation, and now props.
  const all = new Map();
  for (const [k, v] of edges) all.set(k, [...v]);
  for (const [k, v] of extra) all.set(k, [...(all.get(k) ?? []), ...v]);

  const verdicts = new Map();
  // The effects each cell reaches, kept so a later pass can ask which reasons a
  // cell has rather than only which one was recorded first.
  const hits = new Map();
  for (const c of cells.values()) {
    const hit = new Map();
    hits.set(c.key, hit);
    const opaque = new Set();
    const seen = new Set([c.key]);
    const queue = [{ node: c.key, unresolved: false }];
    while (queue.length) {
      const cur = queue.pop();
      for (const e of all.get(cur.node) ?? []) {
        const unresolved = cur.unresolved || e.unresolved;
        if (e.unresolved) opaque.add((e.why ?? "?") + "@" + e.to);
        if (e.to.startsWith("effect:")) {
          if (!hit.has(e.to) || (hit.get(e.to) && !unresolved)) hit.set(e.to, unresolved);
          continue;
        }
        const mark = e.to + (unresolved ? "!" : "");
        if (seen.has(mark)) continue;
        seen.add(mark);
        queue.push({ node: e.to, unresolved });
      }
    }
    let mutation = null;
    for (const [ek, edgeUnresolved] of hit) {
      const e = stateR1.effects.get(ek);
      const mut = e.setup.direct.length || e.setup.scheduled.length ||
        (e.cleanup && !e.cleanup.unresolved && (e.cleanup.direct.length || e.cleanup.scheduled.length));
      if (mut && !edgeUnresolved) mutation ??= ek;
    }
    const openCauses = reachCauses(hit, stateR1.effects, opaque);
    const unresolved = primaryReason(openCauses);
    verdicts.set(c.key, { effects: hit.size, openCauses,
      verdict: mutation ? "MUTATION" : openCauses.length ? "UNRESOLVED" : "READ_ONLY",
      witness: mutation,
      why: mutation ?? unresolved ?? (hit.size ? "reaches only effects that neither mutate nor leave anything open" : "reaches no effect"), cell: c });
  }

  // What the 239 were: only the first class needs an actual/formal edge.
  const kinds = { FORMAL_PROP: 0, MODULE_BINDING: 0, OTHER: 0 };
  for (const o of stateR1.outerDeps) {
    const id = o.owner;
    const path = id.slice(0, id.lastIndexOf("#"));
    if (formals.get(id)?.has(o.name)) kinds.FORMAL_PROP++;
    else if (importsOf.get(path)?.has(o.name) || external.get(path)?.has(o.name) || byComp.get(path + "#?")?.locals.has(o.name)) kinds.MODULE_BINDING++;
    else kinds.OTHER++;
  }
  return { verdicts, formals, stats, kinds, consumers, extra, hits };
})();

export { stateR1c };
