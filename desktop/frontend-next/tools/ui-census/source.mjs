// The tree this census reads, and which of it the product reaches.
import { join, dirname, resolve as resolvePath } from "node:path";
import { parse } from "@babel/parser";
// One authority for what a call is, shared with the repository's own gate.
import { callMode, callSite, isCall } from "../../src/ui/roots.ts";
import { readFileSync, readdirSync, statSync, existsSync } from "node:fs";

// Mutation census, closed in both directions.
//
// The source universe is found rather than filtered by filename: a sink is a
// request that actually carries a mutating verb, wherever it lives. A function
// mutates if it holds a sink or reaches one — a fixpoint over calls, imports
// and class inheritance, so no helper's name establishes anything.
//
// Nothing is ever classified by not being recognised. What cannot be followed
// is UNRESOLVED with a reason, because "I could not tell" and "it does not
// mutate" are different answers and only one of them is safe to act on.

const prod = (dir) =>
  readdirSync(dir).flatMap((n) => {
    const p = join(dir, n);
    if (statSync(p).isDirectory()) return prod(p);
    return /\.tsx?$/.test(n) && !/\.test\./.test(n) ? [p] : [];
  });
const walk = (n, fn) => {
  if (!n || typeof n.type !== "string") return;
  fn(n);
  for (const [k, v] of Object.entries(n)) {
    if (k === "loc" || k.endsWith("Comments")) continue;
    if (Array.isArray(v)) v.forEach((c) => walk(c, fn));
    else walk(v, fn);
  }
};
const walkStack = (node, fn, stack = []) => {
  if (!node || typeof node.type !== "string") return;
  stack.push(node);
  fn(node, stack);
  for (const [k, v] of Object.entries(node)) {
    if (k === "loc" || k.endsWith("Comments")) continue;
    if (Array.isArray(v)) v.forEach((c) => walkStack(c, fn, stack));
    else walkStack(v, fn, stack);
  }
  stack.pop();
};
const flat = (n) => {
  if (!n) return "?";
  if (n.type === "Identifier") return n.name;
  if (n.type === "ThisExpression") return "this";
  // `a?.b` names the same member as `a.b`. Reading only MemberExpression left
  // every optional access spelled "?", which put readable dependency arrays in
  // the unreadable pile and hid the receiver of an optional call.
  if (n.type === "MemberExpression" || n.type === "OptionalMemberExpression") {
    return flat(n.object) + "." + (n.computed ? "[]" : flat(n.property));
  }
  return "?";
};
const jsxName = (n) =>
  n.type === "JSXIdentifier" ? n.name : n.type === "JSXMemberExpression" ? jsxName(n.object) + "." + jsxName(n.property) : "?";
// Every id a value can take. One control is genuinely two actions — send while
// idle, stop while running — and reading only the first literal recorded one of
// them and lost the other.
const allStrings = (n) => {
  const out = [];
  walk(n, (x) => { if (x.type === "StringLiteral") out.push(x.value); });
  return out;
};
const firstString = (n) => {
  let out = null;
  walk(n, (x) => { if (!out && x.type === "StringLiteral") out = x.value; });
  return out;
};

const SRC = process.env.CENSUS_SRC ?? "src";
const FILES = prod(SRC);
const trees = new Map();
for (const p of FILES) trees.set(p, parse(readFileSync(p, "utf8"), { sourceType: "module", plugins: ["typescript", "jsx"], errorRecovery: true }));

function resolveSpec(from, spec) {
  if (!spec.startsWith(".")) return null;
  const base = resolvePath(dirname(from), spec);
  for (const cand of [base + ".ts", base + ".tsx", join(base, "index.ts"), join(base, "index.tsx")]) {
    if (existsSync(cand)) return cand.replace(process.cwd() + "/", "");
  }
  return null;
}
const importsOf = new Map();
const importedFiles = new Map();
const external = new Map();
for (const [path, tree] of trees) {
  const m = new Map();
  const fs = new Set();
  walk(tree, (n) => {
    const spec = (n.type === "ImportDeclaration" || n.type === "ExportNamedDeclaration") && n.source?.value;
    if (spec) {
      const target = resolveSpec(path, spec);
      // A name from outside this tree: a library's, and nothing here declares
      // it. Kept so a React element is not reported as a component chosen at
      // runtime just because its body is not in the scan.
      if (!target) {
        for (const sp of n.specifiers ?? []) if (sp.local?.name) (external.get(path) ?? external.set(path, new Set()).get(path)).add(sp.local.name);
        return;
      }
      fs.add(target);
      for (const sp of n.specifiers ?? []) {
        if (sp.type === "ImportSpecifier") m.set(sp.local.name, { file: target, name: sp.imported.name });
        if (sp.type === "ImportDefaultSpecifier") m.set(sp.local.name, { file: target, name: "default" });
      }
    }
    if (isCall(n) && n.callee?.type === "Import" && n.arguments[0]?.type === "StringLiteral") {
      const target = resolveSpec(path, n.arguments[0].value);
      if (target) fs.add(target);
    }
  });
  importsOf.set(path, m);
  importedFiles.set(path, fs);
}

/** Files reachable from a set of entry modules. */
function reachableFrom(roots) {
  const seen = new Set();
  const queue = roots.filter((r) => trees.has(r));
  while (queue.length) {
    const f = queue.pop();
    if (seen.has(f)) continue;
    seen.add(f);
    for (const next of importedFiles.get(f) ?? []) if (!seen.has(next)) queue.push(next);
  }
  return seen;
}
const PRODUCT_ROOTS = (process.env.CENSUS_ROOTS ?? "src/main.tsx").split(",");
const productFiles = reachableFrom(PRODUCT_ROOTS);


// Which local name each file binds to which React export, and which local names
// stand for the module itself. A wrapper is React's because it was imported
// from react, never because it is spelled memo — the same rule the listener
// registration and the event parameter run under.
const reactImports = new Map();
const reactNamespaces = new Map();
for (const [path, tree] of trees) {
  const m = new Map();
  const ns = new Set();
  walk(tree, (n) => {
    if (n.type !== "ImportDeclaration" || n.source.value !== "react") return;
    for (const sp of n.specifiers) {
      if (sp.type === "ImportSpecifier") m.set(sp.local.name, sp.imported.name);
      else ns.add(sp.local.name);
    }
  });
  reactImports.set(path, m);
  reactNamespaces.set(path, ns);
}

export { callMode, callSite, isCall, reactImports, reactNamespaces, FILES, PRODUCT_ROOTS, SRC, allStrings, external, firstString, flat, importedFiles, importsOf, jsxName, prod, productFiles, reachableFrom, resolveSpec, trees, walk, walkStack };
