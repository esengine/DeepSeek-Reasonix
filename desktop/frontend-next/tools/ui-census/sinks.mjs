// Every site that can change host state, and the fixpoint over which functions
// reach one.
import { SRC, flat, isCall, trees, walkStack } from "./source.mjs";
import { MUTATING, ownerOf } from "./symbols.mjs";
import { NOT_A_MUTATION, SEMANTIC_PAIRS, endpointsOf, excused } from "./endpoints.mjs";
import { transport } from "./transport.mjs";

const sinks = [];
const mutating = new Set();
const transportEdgesNotSinks = [];
for (const [path, tree] of trees) {
  walkStack(tree, (n, stack) => {
    if (!isCall(n) || !/(^|\.)fetch$/.test(flat(n.callee))) return;
    const init = n.arguments[1];
    let verb = null, dynamic = false, indirect = false;
    if (!init) return;
    if (init.type === "ObjectExpression") {
      const prop = init.properties.find((p) => p.type === "ObjectProperty" && p.key?.name === "method");
      if (!prop) return;
      if (prop.value.type === "StringLiteral") verb = prop.value.value;
      else dynamic = true;
    } else {
      // The init is built elsewhere, so the verb is not on this line.
      indirect = true;
    }
    if (verb && !MUTATING.test(verb)) return;
    const named = verb ? endpointsOf(n.arguments[0], path).map((e) => verb + " " + e) : [];
    if (named.length && named.every((e) => e in NOT_A_MUTATION)) {
      excused.push({ path, line: n.loc.start.line, endpoint: named.join("|"), owner: ownerOf(stack) });
      return;
    }
    const owner = ownerOf(stack);
    // A request whose endpoint is its caller's argument is a transport edge:
    // the verb is real and the operation is not this function's. Its call sites
    // are where an endpoint first becomes something one can adjudicate, and 78
    // of this tree's 93 mutating capabilities reach the wire only this way.
    if (owner && transport.edges.has(path + "#" + owner)) {
      transportEdgesNotSinks.push(path + "#" + owner);
      return;
    }
    const how = verb ? "fetch " + verb : dynamic ? "fetch <computed verb>" : "fetch <init built elsewhere>";
    sinks.push({ path, line: n.loc.start.line, how, owner, certain: !!verb, unresolved: dynamic || indirect });
    if (owner) mutating.add(path + "#" + owner);
  });
}

// The endpoint sites the edges hand ownership to. Each is adjudicated on its
// own EndpointKey, so one wrapper carrying a writing endpoint and a probing one
// answers differently for each — and a call whose endpoint nothing here can
// read stays a sink that says so, never a clean one.
for (const site of transport.sites) {
  const key = site.verb + " " + site.path;
  if (key in NOT_A_MUTATION) {
    excused.push({ path: site.at, line: site.line, endpoint: key, owner: site.owner });
    continue;
  }
  sinks.push({ path: site.at, line: site.line, how: "endpoint " + key, owner: site.owner.split("#").pop(), certain: true, unresolved: false });
  mutating.add(site.owner);
}
// A proven mutating verb whose endpoint nothing here can name. It is not a
// mutation — three declared endpoints in this tree carry a mutating verb and
// change nothing — and it is certainly not clean. Recorded as an open edge, so
// whatever reaches it is unresolved rather than either.
const unresolvedTransport = new Set();
for (const d of transport.dynamic) {
  sinks.push({ path: d.at, line: d.line, how: d.verb + " <endpoint not readable>", owner: d.owner.split("#").pop(), certain: true, unresolved: true });
  unresolvedTransport.add(d.owner);
}

for (const k of Object.keys(NOT_A_MUTATION)) {
  if (excused.some((e) => e.endpoint.split("|").includes(k))) continue;
  console.error("declared non-mutation endpoint excuses nothing: " + k);
  process.exitCode = 1;
}
for (const pair of SEMANTIC_PAIRS[SRC] ?? []) {
  const gone = pair.filter((k) => !(k in NOT_A_MUTATION));
  if (gone.length) {
    console.error("semantic pair broken, still adjudicated apart: " + pair.join(" + ") + " lost " + gone.join(", "));
    process.exitCode = 1;
    continue;
  }
  const classes = new Set(pair.map((k) => NOT_A_MUTATION[k].class));
  if (classes.size !== 1) {
    console.error("semantic pair disagrees on class: " + pair.join(" + ") + " -> " + [...classes].join(", "));
    process.exitCode = 1;
  }
}

export { mutating, sinks, transportEdgesNotSinks, unresolvedTransport };
