// Which values this tree proves are Promises, and what a continuation on one
// therefore runs.
//
// The order is the whole point. A value is proven to be a Promise first, and
// only then does `.then` on it mean anything. Going the other way — this was
// called then, so it must have been a Promise — is a circular proof, and the
// member's spelling is not a proof at all: an object with a method called then
// would buy an execution edge into whatever it was handed.
//
// Two sources, and no others. A declared return type is the source stating what
// a call hands back; a continuation on a proven Promise is a Promise by the
// language's own contract, and that is a fixpoint over the receiver chain with
// no hop limit. An opaque call result, an opaque binding, Promise.resolve/all
// and an async function's return are each a separate mechanism with its own
// proof obligation, each measured at zero exclusive payoff before this was
// written: they stay unproven, and the report says which shape each one is.
import { flat, importsOf, isCall, trees, walk, walkStack } from "./source.mjs";
import { declaredType } from "./types.mjs";
import { ownerOf } from "./symbols.mjs";

// What each continuation hands its arguments, by slot. `.then(f, r)` and
// `.catch(r)` deliver the same rejection callback, so they name one edge:
// which member was written is spelling, and what the callback is handed is not.
const CONTINUATION = new Map([
  ["then", ["FULFILLED", "REJECTED"]],
  ["catch", ["REJECTED"]],
  ["finally", ["SETTLED"]],
]);
const EDGE = { FULFILLED: "PromiseThen", REJECTED: "PromiseCatch", SETTLED: "PromiseFinally" };

// The return type a declared interface or type alias states for a member. Only
// a method signature: a function-typed property would be the same evidence
// under a different spelling, and the tree declares none that returns a Promise
// — writing that branch would be building for a case measured at zero.
const returnsOf = new Map();
for (const [path, tree] of trees) {
  const m = new Map();
  walk(tree, (n) => {
    const members =
      n.type === "TSInterfaceDeclaration" ? n.body.body :
      n.type === "TSTypeAliasDeclaration" && n.typeAnnotation?.type === "TSTypeLiteral" ? n.typeAnnotation.members : null;
    if (!members || !n.id?.name) return;
    const fields = new Map();
    for (const f of members) if (f.key?.name && f.type === "TSMethodSignature") fields.set(f.key.name, f.returnType ?? null);
    m.set(n.id.name, fields);
  });
  returnsOf.set(path, m);
}
const membersOfType = (path, name) => {
  const here = returnsOf.get(path)?.get(name);
  if (here) return here;
  const imported = importsOf.get(path)?.get(name);
  return imported ? returnsOf.get(imported.file)?.get(imported.name) ?? null : null;
};

// Promise<T> as the source writes it, and nothing weaker. A union that may not
// be one, an alias no resolver here follows, PromiseLike, unknown, any: each
// would be a claim about a value nobody stated. A tree that declares or imports
// its own Promise means something else by the name, and states nothing here.
const isPromiseType = (path, ann) => {
  const t = ann?.type === "TSTypeAnnotation" ? ann.typeAnnotation : ann;
  if (t?.type !== "TSTypeReference" || flat(t.typeName) !== "Promise") return false;
  return !returnsOf.get(path)?.has("Promise") && !importsOf.get(path)?.has("Promise");
};

/** The shape alone, which proves nothing: which member is written and on what.
 *  Whether that receiver is a Promise is the other half, and requiring it is
 *  what this file exists for. Reads the one authority for what a call is. */
const continuationShape = (node) => {
  const c = isCall(node) ? node.callee : null;
  if (!c?.type?.endsWith("MemberExpression") || c.computed) return null;
  const roles = CONTINUATION.get(c.property?.name ?? "");
  // Where the member is written, not where the expression begins: a chain is
  // one expression, so its every link would otherwise carry the first line.
  return roles ? { member: c.property.name, receiver: c.object, roles, line: c.property.loc.start.line } : null;
};

// A call on a receiver the source types, whose type declares this member
// returns a Promise. The receiver's type comes from the authority that already
// answers it — the same annotation, destructured prop, class field or `new`
// that decides whether a call is a port capability.
const certifiedReturn = (path, node) => {
  const c = node.callee;
  if (!c?.type?.endsWith("MemberExpression") || c.computed) return null;
  const member = c.property?.name;
  const recv = flat(c.object);
  if (!member || recv === "?") return null;
  const t = declaredType(path, recv);
  const decl = t === null ? null : membersOfType(path, t);
  if (!decl || !decl.has(member) || !isPromiseType(path, decl.get(member))) return null;
  return { provenance: "CERTIFIED_API_RETURN", declared: t + "." + member, via: null };
};

// Memoised by node: a fact belongs to the value, not to the path that reached
// it, and one chain is walked once however many continuations hang off it.
const facts = new Map();
/** What proves this expression is a Promise, or null. */
function promiseOf(path, node) {
  if (!isCall(node)) return null;
  if (facts.has(node)) return facts.get(node);
  const shape = continuationShape(node);
  const on = shape ? promiseOf(path, shape.receiver) : null;
  const fact = certifiedReturn(path, node) ??
    (on ? { provenance: "CONTINUATION_RESULT", declared: on.declared, via: on } : null);
  facts.set(node, fact);
  return fact;
}

/** What a fact bottoms out in. Every source above the first one is a
 *  continuation on something already proven, so this is the only place a new
 *  source could enter — which is what the gate checks. */
const rootProvenance = (fact) => (fact.via ? rootProvenance(fact.via) : fact.provenance);

// Why a receiver is not proven: the mechanism a fix would be written against,
// never a verdict. Naming the shape is not claiming it — a binding the source
// types Promise is measured here and deliberately not certified, because the
// binding's declared type is a third source and this cut takes two.
const unprovenShape = (path, recv) => {
  if (!isCall(recv)) {
    const name = flat(recv);
    return declaredType(path, name) === "Promise" ? "DECLARED_BINDING_TYPE"
      : recv.type === "Identifier" ? "OPAQUE_BINDING" : "OPAQUE_VALUE";
  }
  const c = recv.callee;
  if (!c?.type?.endsWith("MemberExpression") || c.computed) return "NOT_A_MEMBER_CALL";
  const t = declaredType(path, flat(c.object));
  if (t === null) return "RECEIVER_NOT_TYPED";
  return membersOfType(path, t)?.has(c.property?.name ?? "") ? "RETURN_NOT_PROMISE" : "MEMBER_NOT_DECLARED";
};

// Every continuation the tree writes, proven or not. Keyed by node for the
// consumers, because the fact and its consumers walk one tree and a lookup by
// name would be a second authority; listed in `sites` for the report, because
// what stays unproven is the measurement this cut is chosen from.
const byNode = new Map();
const sites = [];
for (const [path, tree] of trees) {
  walkStack(tree, (n, stack) => {
    const shape = continuationShape(n);
    if (!shape) return;
    const on = promiseOf(path, shape.receiver);
    const row = {
      node: n, path, line: n.loc.start.line, edgeAt: path + ":" + shape.line,
      comp: ownerOf(stack), member: shape.member, on,
      shape: on ? null : unprovenShape(path, shape.receiver),
      callbacks: shape.roles
        .map((role, i) => ({ role, index: i, arg: n.arguments?.[i] ?? null, edge: EDGE[role] }))
        .filter((c) => c.arg),
    };
    sites.push(row);
    if (on) byNode.set(n, row);
  });
}

// How deep the fixpoint actually ran, so "it converged" is a number rather than
// an assurance: a chain proven at depth d took d receivers to prove.
const depthOf = (fact) => (fact.via ? depthOf(fact.via) + 1 : 1);
const maxDepth = [...byNode.values()].reduce((n, r) => Math.max(n, depthOf(r.on)), 0);

export { CONTINUATION, EDGE, byNode, maxDepth, promiseOf, rootProvenance, sites };
