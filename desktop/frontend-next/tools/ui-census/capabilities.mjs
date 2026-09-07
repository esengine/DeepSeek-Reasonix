// The port members a capability can be, taken from the interface declarations,
// and the files that implement one.
import { flat, isCall, trees, walk, walkStack } from "./source.mjs";
import { chainOf, ownerOf } from "./symbols.mjs";

const callsOf = new Map();
for (const [path, tree] of trees) {
  walkStack(tree, (n, stack) => {
    if (!isCall(n)) return;
    const owner = ownerOf(stack);
    if (!owner) return;
    const key = path + "#" + owner;
    if (!callsOf.has(key)) callsOf.set(key, new Set());
    callsOf.get(key).add(flat(n.callee));
  });
}
// A component calls the kernel through an interface-typed prop, so neither an
// import nor a class chain names the implementation. The interface declaration
// does: a member of AgentPort or HubPort is a capability, and which class
// implements it is found by `implements`, not by where the file sits.
const capabilities = new Set();
for (const [path, tree] of trees) {
  walk(tree, (n) => {
    if (n.type !== "TSInterfaceDeclaration" || !/^(AgentPort|HubPort)$/.test(n.id?.name ?? "")) return;
    for (const m of n.body.body) {
      const key = m.key?.name;
      if (key && (m.type === "TSMethodSignature" || m.type === "TSPropertySignature")) capabilities.add(key);
    }
  });
}
const implFiles = new Set();
for (const [path, tree] of trees) {
  walk(tree, (n) => {
    if (n.type !== "ClassDeclaration" || !n.implements) return;
    if (n.implements.some((i) => /^(AgentPort|HubPort)$/.test(i.expression?.name ?? ""))) {
      for (const f of chainOf.get(path) ?? []) implFiles.add(f);
    }
  });
}

export { callsOf, capabilities, implFiles };
