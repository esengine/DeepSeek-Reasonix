// What the platform hands a certified interaction root's callback.
//
// The identity is the binding, never the spelling. A handler declared
// `(whatever) => whatever.preventDefault()` receives the event; an ordinary
// helper whose parameter happens to be called `e` does not, and an inner
// function that rebinds the name owns it from there down. Nothing in this file
// reads a parameter's name to decide what it is.
import { census } from "./certified.mjs";
import { bindingInScope, bindsName } from "../../src/ui/roots.ts";
import { flat, isCall, trees, walk, walkStack } from "./source.mjs";

const FN = /^(ArrowFunctionExpression|FunctionExpression|FunctionDeclaration)$/;

// What may be proved about the event object, and nothing beyond it. These are
// declared rather than inferred, and the declaration buys nothing on its own:
// it applies only where the receiver is already proven to be the event this
// certified root receives, which is what makes it different from a table of
// member names. Suppressing a default action and stopping propagation are
// operations on the dispatch in flight; neither writes canonical host state.
//
// Only what the corpus actually calls is listed. An event member that is not
// here stays unresolved and says so — the receiver being proven does not make
// everything reachable through it proven.
const EVENT_CONTROL = new Set(["preventDefault", "stopPropagation"]);

// The functions a file declares by name, keeping the function itself: a
// handler written `onClick={save}` runs the declaration, and its parameters are
// what the event is delivered to.
const declaredFns = new Map();
const fnsOf = (path) => {
  if (declaredFns.has(path)) return declaredFns.get(path);
  const m = new Map();
  walk(trees.get(path), (n) => {
    if (n.type === "VariableDeclarator" && n.id?.type === "Identifier" && FN.test(n.init?.type ?? "")) m.set(n.id.name, n.init);
    if (n.type === "FunctionDeclaration" && n.id) m.set(n.id.name, n);
  });
  declaredFns.set(path, m);
  return m;
};

const callbackFn = (cb, path) => {
  if (!cb) return null;
  if (FN.test(cb.type)) return cb;
  if (cb.type === "Identifier") return fnsOf(path).get(cb.name) ?? null;
  return null;
};

// One per certified root whose callback takes the event. Parameter zero,
// because that is what a JSX handler prop and addEventListener both deliver;
// a source whose contract says otherwise would have to say so here.
const facts = [];
for (const r of census.roots) {
  if (!r.event || !r.callback) continue;
  const fn = callbackFn(r.callback, r.path);
  const p0 = (fn?.params ?? [])[0];
  if (!p0 || p0.type !== "Identifier") continue;
  facts.push({ callback: fn, paramIndex: 0, binding: p0.name, eventKind: r.event,
    sourceKind: r.kind, sourceRoot: r.path + ":" + r.line, path: r.path, comp: r.comp,
    provenance: r.kind === "jsx-handler" ? "jsx-handler-prop" : "listener-registration" });
}

const baseOf = (n) => {
  let x = n;
  while (x?.type?.endsWith("MemberExpression")) x = x.object;
  return x?.type === "Identifier" ? x.name : null;
};

// Every call written on that parameter, with the inner scopes that take the
// name back excluded. Keyed by node, because the fact and its consumers walk
// one tree and a second lookup by name would be a second authority.
//
// Three answers, not one. The receiver being the event proves what the event
// contract proves and no more: `e.preventDefault()` is the event, and
// `e.currentTarget.blur()` is whatever currentTarget is — a receiver this pass
// has not typed, and one whose provenance is a separate debt.
const byCall = new Map();
for (const f of facts) {
  walkStack(f.callback, (n, stack) => {
    if (!isCall(n)) return;
    const callee = n.callee;
    if (!callee?.type?.endsWith("MemberExpression") || callee.computed) return;
    if (baseOf(callee) !== f.binding) return;
    // Between the handler and this call, anything that binds the name owns it.
    const inner = stack.slice(1).filter((x) => x !== f.callback);
    if (bindingInScope(f.binding, inner)) return;
    const onParam = callee.object?.type === "Identifier" && callee.object.name === f.binding;
    const member = callee.property?.name ?? "?";
    byCall.set(n, { fact: f, member, onParam,
      proven: onParam && EVENT_CONTROL.has(member),
      kind: !onParam ? "event-param-path-receiver-unproven"
        : EVENT_CONTROL.has(member) ? null : "event-param-member-unresolved" });
  });
}

export { EVENT_CONTROL, bindsName, byCall, facts };
