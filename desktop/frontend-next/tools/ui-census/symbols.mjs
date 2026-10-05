// Who owns a scope, what a call's receiver is named, and which file a class
// chain reaches.
import { FILES, importsOf, trees, walk } from "./source.mjs";

// Ordered above the sinks because the endpoint graph is built before them:
// which function owns a request is decided by where its endpoint is named,
// and that cannot be answered after ownership has already been assigned.

const MUTATING = /^(POST|PUT|PATCH|DELETE)$/;
function ownerOf(stack) {
  for (let i = stack.length - 1; i >= 0; i--) {
    const n = stack[i];
    if (n.type === "ClassMethod" && n.key?.name) return n.key.name;
    if (n.type === "FunctionDeclaration" && n.id) return n.id.name;
    if (n.type === "ObjectMethod" && n.key?.name) return n.key.name;
    // A function expression that names itself owns that scope, wherever it is
    // written. `const Row = memo(function Row(){…})` reaches ownerOf through a
    // CallExpression, not a VariableDeclarator, so reading only the declarator
    // left every render site inside Row with no enclosing component and cut the
    // prop chain there.
    if (n.type === "FunctionExpression" && n.id?.name) return n.id.name;
    if (n.type === "ArrowFunctionExpression" || n.type === "FunctionExpression") {
      const p = stack[i - 1];
      if (p?.type === "VariableDeclarator" && p.id?.type === "Identifier") return p.id.name;
      if (p?.type === "ClassProperty" && p.key?.name) return p.key.name;
    }
  }
  return null;
}

// What a function is called where it is written: its own id when it has one,
// otherwise the binding it is assigned to. ownerOf, the props walk and the
// presence walk all have to answer this the same way, or a component's body and
// its render site stop being the same component.
const declaredName = (fn, parent) =>
  (fn.type === "FunctionDeclaration" || fn.type === "FunctionExpression") && fn.id?.name ? fn.id.name
  : parent?.type === "VariableDeclarator" && parent.id?.type === "Identifier" ? parent.id.name : null;

// The names an expression reads, with member properties left out: `a.b` is read
// when a changes, and `b` is not a binding of its own.
const baseNamesOf = (n) => {
  const out = new Set();
  const visit = (x) => {
    if (!x || typeof x.type !== "string") return;
    if (x.type === "Identifier") return void out.add(x.name);
    // A type is not read at runtime. Walking into annotations put HTMLDivElement
    // in the list of bindings an expression depends on.
    if (x.type.startsWith("TSType") || x.type === "TSTypeAnnotation" || x.type === "TypeAnnotation") return;
    if (x.type === "MemberExpression" || x.type === "OptionalMemberExpression") {
      visit(x.object);
      if (x.computed) visit(x.property);
      return;
    }
    // A property name is not a value read either: `{ fixed: true }` reads
    // nothing, and counting its key made an object literal look like it
    // depended on a binding called fixed.
    if (x.type === "ObjectProperty" || x.type === "ObjectMethod" || x.type === "ClassProperty" || x.type === "ClassMethod") {
      if (x.computed) visit(x.key);
      visit(x.value ?? x.body);
      return;
    }
    for (const [k, v] of Object.entries(x)) {
      if (k === "loc" || k === "typeParameters" || k === "typeArguments" || k === "returnType" || k.endsWith("Comments")) continue;
      if (Array.isArray(v)) v.forEach(visit);
      else visit(v);
    }
  };
  visit(n);
  return out;
};

// A method may live on a base class several files away. The transport chain in
// this tree is ten deep, and a walk that stopped at eight reported the port's
// own mutations as non-mutating — the hop limit had quietly become the answer.
const chainOf = new Map();
for (const path of FILES) {
  const chain = new Set([path]);
  const supers = [];
  const collect = (file) => {
    const t = trees.get(file);
    if (!t) return;
    walk(t, (n) => {
      if (n.type === "ClassDeclaration" && n.superClass?.type === "Identifier") supers.push({ file, name: n.superClass.name });
    });
  };
  collect(path);
  for (let guard = 0; supers.length; guard++) {
    if (guard > FILES.length) { chain.add("__unresolved:inheritance-cycle"); break; }
    const { file, name } = supers.pop();
    const imported = importsOf.get(file)?.get(name);
    if (!imported) continue;
    if (chain.has(imported.file)) continue;
    chain.add(imported.file);
    collect(imported.file);
  }
  chainOf.set(path, chain);
}

const resolveCallee = (file, name) => {
  const local = name.split(".")[0];
  const out = [];
  if (name.startsWith("this.")) {
    const member = name.split(".")[1];
    for (const f of chainOf.get(file) ?? []) out.push(f + "#" + member);
  }
  out.push(file + "#" + local);
  const imported = importsOf.get(file)?.get(local);
  if (imported) out.push(imported.file + "#" + imported.name);
  return out;
};

export { MUTATING, baseNamesOf, chainOf, declaredName, ownerOf, resolveCallee };
