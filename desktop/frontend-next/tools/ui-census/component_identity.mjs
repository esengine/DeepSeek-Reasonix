// Which component body a render target names.
//
// `export const Pane = memo(PaneView)` gives one component two names: `Pane` is
// what JSX writes and `PaneView` is where the body, its props and its effects
// live. Every reader that indexes by one and asks by the other loses the edge —
// the lifecycle pass learned this once and kept the answer to itself, so the
// prop graph went on rebuilding identity from the element's spelling and found
// no render site for ten props that are passed on every render.
//
// One construction, both directions. The body stays canonical: an alias is a
// certified render target *of* a body, never a second name a body may be
// confused with, because two files spelling a component `Row` are two
// components and a name index once wired one into the other.
import { flat, isCall, reactImports, reactNamespaces, trees, walk } from "./source.mjs";

// The wrapper is React's because the file imported it from react, not because
// it is spelled memo. A local function of that name is a local function, and a
// render target it produces names nothing this pass may resolve.
const REACT_WRAPPER = /^(memo|forwardRef)$/;
const wrapperKind = (path, callee) => {
  const named = reactImports.get(path)?.get(callee);
  if (named && REACT_WRAPPER.test(named)) return named;
  const m = /^([A-Za-z_$][\w$]*)\.(memo|forwardRef)$/.exec(callee);
  return m && reactNamespaces.get(path)?.has(m[1]) ? m[2] : null;
};

// `const X = w(inner)`, where the chain is wrappers React defines. A named
// function expression is a body whatever it was handed to; an identifier
// through an arbitrary call is not.
const alias = new Map();
const wrapperOf = new Map();
for (const [path, tree] of trees) {
  walk(tree, (n) => {
    if (n.type !== "VariableDeclarator" || n.id?.type !== "Identifier" || !isCall(n.init)) return;
    let arg = n.init;
    const wraps = [];
    while (isCall(arg) && arg.arguments.length) { wraps.push(wrapperKind(path, flat(arg.callee))); arg = arg.arguments[0]; }
    const inner = arg?.type === "FunctionExpression" && arg.id?.name ? arg.id.name
      : arg?.type === "Identifier" && wraps.every(Boolean) ? arg.name : null;
    if (arg === n.init || !inner || inner === n.id.name) return;
    alias.set(path + "#" + n.id.name, path + "#" + inner);
    wrapperOf.set(path + "#" + n.id.name, wraps.includes("memo") ? "memo" : "");
  });
}

/** The body a render target resolves to, to a fixed point. */
const bodyOf = (id) => {
  const chain = new Set([id]);
  while (alias.has(id) && !chain.has(alias.get(id))) { id = alias.get(id); chain.add(id); }
  return id;
};

/** The render targets that name a body — the reverse edge, built from the same
 *  map so the two directions cannot disagree. */
const targets = new Map();
for (const from of alias.keys()) {
  const to = bodyOf(from);
  if (!targets.has(to)) targets.set(to, []);
  targets.get(to).push(from);
}
const renderTargetsOf = (body) => targets.get(body) ?? [];

/** Bodies reached through `memo`, whose parent's render stops at them when
 *  every prop it passes is unchanged. */
const memoized = new Set();
for (const [from, to] of alias) if (wrapperOf.get(from) === "memo") memoized.add(to);

export { alias, bodyOf, memoized, renderTargetsOf, wrapperOf };
