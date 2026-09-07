// Transport-R1: which function owns an endpoint. A wrapper that takes the
// path as a parameter owns none of them; its callers do.
import { flat, isCall, trees, walk, walkStack } from "./source.mjs";
import { MUTATING, baseNamesOf, declaredName, ownerOf, resolveCallee } from "./symbols.mjs";
import { endpointOf, endpointsOf } from "./endpoints.mjs";

// A verb on a fetch says a request was made; it does not say what operation it
// is. Where the endpoint is the caller's argument, the function holding the
// fetch is a transport edge and not the operation: `post0(path, body)` is five
// lines of plumbing that 78 of this tree's 93 mutating capabilities go through,
// so adjudicating an endpoint could only ever reach the other 15.
//
// Ownership therefore moves to the first call site that supplies an endpoint
// identity something can be said about. It was built beside the old graph and
// compared before it was switched on: a refactor that both moves ownership and
// changes answers cannot say which of the two produced a difference. The five
// wrappers stay in the graph — they carry the transport provenance an endpoint
// site is only reachable through — and stop being sinks.
const transport = (() => {
  const paramsOf = new Map();
  const fnAt = new Map();
  for (const [path, tree] of trees) {
    walkStack(tree, (n, stack) => {
      if (!/^(FunctionDeclaration|FunctionExpression|ArrowFunctionExpression|ClassMethod|ObjectMethod)$/.test(n.type)) return;
      const name = n.type === "ClassMethod" || n.type === "ObjectMethod"
        ? n.key?.name : declaredName(n, stack[stack.length - 2]);
      if (!name) return;
      const key = path + "#" + name;
      if (!paramsOf.has(key)) paramsOf.set(key, (n.params ?? []).map((p) => (p.type === "Identifier" ? p.name : null)));
      fnAt.set(key, n);
    });
  }
  // An edge: a mutating fetch whose endpoint arrives as this function's own
  // parameter. Recorded with which parameter carries it, because that is what a
  // call site has to supply for the endpoint to become knowable.
  const edges = new Map();
  const inlineSites = [];
  for (const [path, tree] of trees) {
    walkStack(tree, (n, stack) => {
      if (!isCall(n) || !/(^|\.)fetch$/.test(flat(n.callee))) return;
      const init = n.arguments[1];
      const prop = init?.type === "ObjectExpression"
        ? init.properties.find((p) => p.type === "ObjectProperty" && p.key?.name === "method") : null;
      const verb = prop?.value?.type === "StringLiteral" ? prop.value.value : null;
      if (!verb || !MUTATING.test(verb)) return;
      const owner = ownerOf(stack);
      if (!owner) return;
      const key = path + "#" + owner;
      const literal = endpointOf(n.arguments[0], path);
      if (literal !== null) { inlineSites.push({ owner: key, verb, path: literal, how: "inline" }); return; }
      const names = baseNamesOf(n.arguments[0]);
      const ps = paramsOf.get(key) ?? [];
      const idx = ps.findIndex((p) => p && names.has(p));
      if (idx >= 0) edges.set(key, { verb, param: idx, via: "own-parameter" });
      else inlineSites.push({ owner: key, verb, path: null, how: "unreadable" });
      void names;
    });
  }
  // A wrapper handed to a wrapper is one too, at whichever parameter it passes
  // on. A fixpoint, not a hop limit: the chain's length is the tree's business.
  for (let moved = true; moved; ) {
    moved = false;
    for (const [key, fn] of fnAt) {
      if (edges.has(key)) continue;
      const ps = paramsOf.get(key) ?? [];
      let found = null;
      walk(fn, (n) => {
        if (found || !isCall(n)) return;
        for (const target of resolveCallee(key.slice(0, key.lastIndexOf("#")), flat(n.callee))) {
          const e = edges.get(target);
          if (!e) continue;
          const arg = n.arguments[e.param];
          if (!arg) continue;
          // Forwarding is passing the path on untouched. A function that puts
          // a literal into it is supplying an endpoint, not relaying one, and
          // reading `"/inbox/items/" + id` as a relay lost nine capabilities.
          if (endpointsOf(arg, key.slice(0, key.lastIndexOf("#"))).length) continue;
          const idx = ps.findIndex((p) => p && baseNamesOf(arg).has(p));
          if (idx >= 0) found = { verb: e.verb, param: idx, via: "forwards to " + target };
        }
      });
      if (found) { edges.set(key, found); moved = true; }
    }
  }
  // The call sites where an endpoint first becomes something one can speak of.
  const sites = [];
  const dynamic = [];
  for (const [path, tree] of trees) {
    walkStack(tree, (n, stack) => {
      if (!isCall(n)) return;
      const owner = ownerOf(stack);
      if (!owner) return;
      const key = path + "#" + owner;
      if (edges.has(key)) return;
      for (const target of resolveCallee(path, flat(n.callee))) {
        const e = edges.get(target);
        if (!e) continue;
        const arg = n.arguments[e.param];
        const named = arg ? endpointsOf(arg, path) : [];
        if (named.length) for (const p of named) sites.push({ owner: key, verb: e.verb, path: p, through: target, at: path, line: n.loc.start.line });
        else dynamic.push({ owner: key, verb: e.verb, through: target, at: path, line: n.loc.start.line, why: "DYNAMIC_ENDPOINT_UNRESOLVED" });
        return;
      }
    });
  }
  return { edges, sites, inlineSites, dynamic, paramsOf };
})();

export { transport };
