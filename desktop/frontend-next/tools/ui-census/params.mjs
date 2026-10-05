// Whether a function executes a parameter it was handed, so a callback passed
// in is followed only where it is actually called.
import { flat, isCall, trees, walkStack } from "./source.mjs";
import { ownerOf, resolveCallee } from "./symbols.mjs";

// Passing a callback and calling one are different edges. A fixpoint over
// "position i of F is called" answers both directly (fn()) and through one
// more hop (F hands its parameter to G, and G calls that position).
const params = new Map();   // file#name -> [param names]
const paramCalls = new Map(); // file#name -> Set(index) called directly
const paramPasses = new Map(); // file#name -> [{index, to, toIndex}]
for (const [path, tree] of trees) {
  walkStack(tree, (n, stack) => {
    const isFn = n.type === "FunctionDeclaration" || n.type === "ArrowFunctionExpression" || n.type === "FunctionExpression";
    if (!isFn) return;
    const owner = ownerOf(stack);
    if (!owner) return;
    const key = path + "#" + owner;
    if (params.has(key)) return;
    const names = n.params.map((p) => (p.type === "Identifier" ? p.name : null));
    params.set(key, names);
    const called = new Set();
    const passes = [];
    // Same boundary as everywhere else: a parameter called inside a closure
    // this function returns is not called by this function. wrap(fn) does not
    // run fn; whatever wrap returns might.
    const runs = (node, fn) => {
      if (!node || typeof node.type !== "string") return;
      fn(node);
      for (const [k, v] of Object.entries(node)) {
        if (k === "loc" || k.endsWith("Comments")) continue;
        const nested = (c) => c && typeof c.type === "string" &&
          (c.type === "ArrowFunctionExpression" || c.type === "FunctionExpression" || c.type === "FunctionDeclaration");
        const visit = (c) => { if (c && typeof c.type === "string" && !nested(c)) runs(c, fn); };
        if (Array.isArray(v)) v.forEach(visit);
        else visit(v);
      }
    };
    runs(n.body, (c) => {
      if (!isCall(c)) return;
      const callee = flat(c.callee);
      const i = names.indexOf(callee);
      if (i >= 0) { called.add(i); return; }
      c.arguments.forEach((a, j) => {
        if (a.type !== "Identifier") return;
        const k = names.indexOf(a.name);
        if (k >= 0) passes.push({ index: k, to: callee, toIndex: j });
      });
    });
    paramCalls.set(key, called);
    paramPasses.set(key, passes);
  });
}
const callsParam = (key, i) => {
  if (paramCalls.get(key)?.has(i)) return true;
  for (const p of paramPasses.get(key) ?? []) {
    if (p.index !== i) continue;
    const file = key.slice(0, key.lastIndexOf("#"));
    for (const target of resolveCallee(file, p.to)) if (paramCalls.get(target)?.has(p.toIndex)) return true;
  }
  return false;
};

export { callsParam, paramCalls, paramPasses, params };
