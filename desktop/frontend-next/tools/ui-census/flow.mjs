// Where a value came from: parameter initialisers, local initialisers, and the
// formal/actual pairing a call site makes.
import { flat, isCall, productFiles, trees, walk, walkStack } from "./source.mjs";
import { declaredName, ownerOf } from "./symbols.mjs";
import { stateSetters } from "./classify.mjs";

const ITERATOR = /\.(map|flatMap|filter|forEach|find|findLast|findIndex|some|every|reduce|sort|flat)$/;
const paramInits = new Map();
for (const [path, tree] of trees) {
  if (!productFiles.has(path)) continue;
  walkStack(tree, (n, stack) => {
    if (!isCall(n) || !n.arguments.length) return;
    const fn = n.arguments[0];
    if (fn?.type !== "ArrowFunctionExpression" && fn?.type !== "FunctionExpression") return;
    const p0 = fn.params?.[0];
    if (p0?.type !== "Identifier") return;
    const callee = flat(n.callee);
    const owner = path + "#" + (ownerOf(stack) ?? "?") + "#" + p0.name;
    if (ITERATOR.test(callee) && n.callee.object) paramInits.set(owner, { kind: "expr", node: n.callee.object });
    else if (stateSetters.has(path + "#" + callee)) paramInits.set(owner, { kind: "state", setter: callee });
  });
}

// What each binding in a component is initialised from, so a derived local can
// be read rather than blanketed.
const localInits = new Map();
for (const [path, tree] of trees) {
  if (!productFiles.has(path)) continue;
  walkStack(tree, (n, stack) => {
    if (n.type !== "VariableDeclarator" || !n.init) return;
    const owner = path + "#" + (ownerOf(stack) ?? "?");
    walk(n.id, (b) => { if (b.type === "Identifier") localInits.set(owner + "#" + b.name, n.init); });
  });
}

// What a component takes from outside: destructured props and positional
// parameters both. A hook is fed by arguments where a component is fed by JSX
// attributes, and both meet a dependency inside the body the same way.
const formals = new Map();
const positional = new Map();
for (const [path, tree] of trees) {
  if (!productFiles.has(path)) continue;
  walkStack(tree, (n, stack) => {
    if (n.type !== "FunctionDeclaration" && n.type !== "ArrowFunctionExpression" && n.type !== "FunctionExpression") return;
    const parent = stack[stack.length - 2];
    const name = declaredName(n, parent);
    if (!name) return;
    const id = path + "#" + name;
    (n.params ?? []).forEach((prm, i) => {
      if (!formals.has(id)) formals.set(id, new Map());
      if (prm.type === "Identifier") { formals.get(id).set(prm.name, "@" + i); positional.set(id + "@" + i, prm.name); return; }
      if (prm.type !== "ObjectPattern") return;
      for (const pr of prm.properties) {
        if (pr.type !== "ObjectProperty" || pr.key?.type !== "Identifier") continue;
        const local = pr.value?.type === "Identifier" ? pr.value.name : pr.key.name;
        formals.get(id).set(local, pr.key.name);
      }
    });
  });
}

export { ITERATOR, formals, localInits, paramInits, positional };
