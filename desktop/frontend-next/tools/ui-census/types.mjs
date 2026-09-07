// Receiver types, taken from where the source states them, so a capability is
// a port member rather than a name that happens to match one.
import { flat, importsOf, trees, walk } from "./source.mjs";
import { callsOf, capabilities, implFiles } from "./capabilities.mjs";
import { mutating } from "./sinks.mjs";
import { resolveCallee } from "./symbols.mjs";

// A capability is a member of AgentPort or HubPort, and a member name alone
// does not say what is being called: EventSource.close and AgentPort.close are
// the same six letters, and matching on them credited a stream teardown with
// every mutation the port's close performs. What the receiver is comes from
// the annotation the source writes — a parameter's, a destructured prop's, a
// class field's, a `new` — never from the spelling. Where nothing states it,
// the call is open: not credited, and not dismissed either.
const PORT_TYPE = /^(AgentPort|HubPort)$/;
const typeNameOf = (t) => {
  if (!t) return null;
  if (t.type === "TSTypeAnnotation") return typeNameOf(t.typeAnnotation);
  if (t.type === "TSTypeReference") return flat(t.typeName);
  if (t.type === "TSUnionType") {
    const named = t.types.map(typeNameOf).filter((x) => x !== null);
    return named.length === 1 ? named[0] : null;
  }
  if (t.type === "TSNullKeyword" || t.type === "TSUndefinedKeyword") return null;
  if (t.type === "TSTypeLiteral") return null;
  return "\u0000other:" + t.type;
};
// The first type argument an annotation carries: `RefObject<HTMLDivElement>`
// names the node the ref holds, which is what says whether a listener is on a
// DOM target at all.
const typeArgOf = (ann) => {
  const t = ann?.type === "TSTypeAnnotation" ? ann.typeAnnotation : ann;
  const a = t?.typeParameters ?? t?.typeArguments;
  return a?.params?.length ? typeNameOf(a.params[0]) : null;
};
// name -> its members' type names, for the shapes a props annotation points at.
const shapesOf = new Map();
const shapeArgs = new Map();
for (const [path, tree] of trees) {
  const m = new Map();
  walk(tree, (n) => {
    const members =
      n.type === "TSInterfaceDeclaration" ? n.body.body :
      n.type === "TSTypeAliasDeclaration" && n.typeAnnotation?.type === "TSTypeLiteral" ? n.typeAnnotation.members : null;
    if (!members || !n.id?.name) return;
    const fields = new Map();
    for (const f of members) {
      if (!f.key?.name) continue;
      if (f.type === "TSPropertySignature") {
        fields.set(f.key.name, typeNameOf(f.typeAnnotation));
        const t = typeArgOf(f.typeAnnotation);
        if (t) shapeArgs.set(n.id.name + "#" + f.key.name, t);
      }
      // A method member names no type, and membership is what a view is judged on.
      else if (f.type === "TSMethodSignature") fields.set(f.key.name, null);
    }
    m.set(n.id.name, fields);
  });
  shapesOf.set(path, m);
}
const shapeArgsFor = (path, ann) => {
  const t = ann?.type === "TSTypeAnnotation" ? ann.typeAnnotation : ann;
  const out = new Map();
  if (t?.type === "TSTypeLiteral") {
    for (const f of t.members) {
      if (f.type !== "TSPropertySignature" || !f.key?.name) continue;
      const a = typeArgOf(f.typeAnnotation);
      if (a) out.set(f.key.name, a);
    }
    return out;
  }
  if (t?.type !== "TSTypeReference") return out;
  const name = flat(t.typeName);
  const decl = importsOf.get(path)?.get(name)?.name ?? name;
  for (const [k, v] of shapeArgs) if (k.startsWith(decl + "#")) out.set(k.slice(decl.length + 1), v);
  return out;
};
const shapeMembers = (path, ann) => {
  if (!ann) return null;
  const t = ann.type === "TSTypeAnnotation" ? ann.typeAnnotation : ann;
  if (t.type === "TSTypeLiteral") {
    const fields = new Map();
    for (const f of t.members) {
      if (!f.key?.name) continue;
      if (f.type === "TSPropertySignature") fields.set(f.key.name, typeNameOf(f.typeAnnotation));
      else if (f.type === "TSMethodSignature") fields.set(f.key.name, null);
    }
    return fields;
  }
  if (t.type !== "TSTypeReference") return null;
  const name = flat(t.typeName);
  const here = shapesOf.get(path)?.get(name);
  if (here) return here;
  const imported = importsOf.get(path)?.get(name);
  return imported ? shapesOf.get(imported.file)?.get(imported.name) ?? null : null;
};
// A name bound twice to different types in one file states nothing here, so it
// is recorded as a conflict rather than resolved by whichever came last.
const CONFLICT = "\u0000conflict";
const bindingTypes = new Map();
const bindingTypeArgs = new Map();
for (const [path, tree] of trees) {
  const b = new Map();
  const targs = bindingTypeArgs;
  const bind = (name, type, arg) => {
    if (arg) targs.set(path + "#" + name, arg);
    if (!name || type === null || type === undefined) return;
    b.set(name, b.has(name) && b.get(name) !== type ? CONFLICT : type);
  };
  const firstArg = typeArgOf;
  const params = (fn) => {
    for (const prm of fn.params ?? []) {
      if (prm.type === "Identifier") { bind(prm.name, typeNameOf(prm.typeAnnotation), firstArg(prm.typeAnnotation)); continue; }
      if (prm.type !== "ObjectPattern") continue;
      const fields = shapeMembers(path, prm.typeAnnotation);
      const fieldArgs = shapeArgsFor(path, prm.typeAnnotation);
      if (!fields) continue;
      for (const pr of prm.properties) {
        if (pr.type !== "ObjectProperty" || pr.key?.type !== "Identifier") continue;
        if (pr.value?.type === "Identifier") bind(pr.value.name, fields.get(pr.key.name) ?? null, fieldArgs.get(pr.key.name) ?? null);
      }
    }
  };
  walk(tree, (n) => {
    if (n.type === "FunctionDeclaration" || n.type === "FunctionExpression" || n.type === "ArrowFunctionExpression" || n.type === "ClassMethod") params(n);
    if (n.type === "VariableDeclarator" && n.id?.type === "Identifier") {
      bind(n.id.name, typeNameOf(n.id.typeAnnotation), firstArg(n.id.typeAnnotation));
      // A constructed object is its constructor, which is provenance the same
      // way an annotation is.
      if (n.init?.type === "NewExpression" && n.init.callee?.type === "Identifier") bind(n.id.name, n.init.callee.name);
    }
    if (n.type === "ClassProperty" && n.key?.name) bind("this." + n.key.name, typeNameOf(n.typeAnnotation));
    if (n.type === "ClassDeclaration" && n.implements?.some((x) => PORT_TYPE.test(x.expression?.name ?? ""))) {
      bind("this", x_portName(n));
    }
  });
  bindingTypes.set(path, b);
}
function x_portName(n) {
  const hit = n.implements.find((x) => PORT_TYPE.test(x.expression?.name ?? ""));
  return hit.expression.name;
}
// A narrowed view of the port is still the port: a component that declares it
// takes `{ probeProvider, saveProvider, ... }` rather than the whole interface,
// and the object handed to it is a port implementation either way. So a type is
// judged by the members it declares, not by being spelled AgentPort. A type the
// tree neither declares nor imports from itself is the platform's — that is how
// an EventSource is told from a port without a list of platform class names.
const kindOfType = (path, t) => {
  if (PORT_TYPE.test(t)) return "PORT";
  const imported = importsOf.get(path)?.get(t);
  const shape = shapesOf.get(path)?.get(t) ?? (imported ? shapesOf.get(imported.file)?.get(imported.name) : null);
  if (shape) return shape.size > 0 && [...shape.keys()].every((m) => capabilities.has(m)) ? "PORT" : "OTHER";
  if (!imported || !trees.has(imported.file)) return "OTHER";
  return "UNPROVEN";
};
/** PORT, OTHER or UNPROVEN — what the source says the receiver is. */
const receiverKind = (path, recv) => {
  const t = bindingTypes.get(path)?.get(recv);
  if (t === undefined || t === null || t === CONFLICT || t.startsWith("\u0000")) return "UNPROVEN";
  return kindOfType(path, t);
};

/** Whether a capability's implementation mutates, across the classes that
 *  declare they implement the port. */
const capabilityMutates = (name) => {
  for (const f of implFiles) if (mutating.has(f + "#" + name)) return f + "#" + name;
  return null;
};

for (let moved = true; moved; ) {
  moved = false;
  for (const [key, called] of callsOf) {
    if (mutating.has(key)) continue;
    const file = key.slice(0, key.lastIndexOf("#"));
    for (const name of called) {
      const member = name.includes(".") ? name.split(".").pop() : null;
      // Only a receiver the source types as a port carries a port capability.
      // An unproven one adds nothing here; classify records it as open, which
      // is where an unknown belongs — this set is what has been proven.
      const port = member && capabilities.has(member) &&
        receiverKind(file, name.slice(0, name.lastIndexOf("."))) === "PORT";
      if ((port && capabilityMutates(member)) ||
          resolveCallee(file, name).some((k) => mutating.has(k))) { mutating.add(key); moved = true; break; }
    }
  }
}

export { CONFLICT, PORT_TYPE, bindingTypeArgs, bindingTypes, capabilityMutates, kindOfType, receiverKind, shapeArgs, shapeArgsFor, shapeMembers, shapesOf, typeArgOf, typeNameOf, x_portName };
