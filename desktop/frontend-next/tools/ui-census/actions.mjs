// Root to action membership. Identity is the product's, read from a literal it
// declares, and joined from the roots alone.
import { join, dirname, resolve as resolvePath } from "node:path";
import { parse } from "@babel/parser";
import { readFileSync, readdirSync, statSync, existsSync } from "node:fs";
import { SRC, walk } from "./source.mjs";
import { roots } from "./roots.mjs";
import { stateR2 } from "./verdicts.mjs";

// Identity is the product's to own, not the analyzer's. Membership comes from a
// literal the source declares — a data-action attribute, a shortcut table's
// action field — and from nothing else. Two roots that call the same handler,
// carry the same label, reach the same port method or hit the same endpoint are
// not thereby one action: that is "a name is not an identity" arriving one
// layer up, and it would have the analyzer invent vocabulary the product never
// wrote.
//
// A forwarded declaration would be a second authority — a render site naming
// the action for the roots inside the component it renders — and it is declared
// here as a state rather than assumed away. It is empty in this tree: no
// data-action in the product sits anywhere but on a root.
//
// Sharing a production binding was tried as evidence and does not work. It is
// reported, not used: a root wired through the same actual argument as five
// declared actions has met the shared UI primitive they all pass through, not
// their intent — and the one case it was built for, RemoteAsk's Escape reaching
// the same onAnswer its reject button does, it never even matched, because the
// two components are rendered at different sites. So an undeclared root is
// recorded as UNDECLARED: not "belongs to no action", but "the product does not
// name this one", which is a fact about the product and sometimes a gap in it.
const REGISTRY = (() => {
  // Anchored to the tree being scanned, like every other path here: a literal
  // "src/actions.ts" would load the product's registry into a fixture run and
  // report all 47 of its entries as having no production root.
  const at = join(SRC, "actions.ts");
  const src = existsSync(at) ? readFileSync(at, "utf8") : null;
  if (!src) return null;
  const tree = parse(src, { sourceType: "module", plugins: ["typescript"], errorRecovery: true });
  const out = new Map();
  walk(tree, (n) => {
    if (n.type !== "ObjectExpression") return;
    const props = n.properties.filter((x) => x.type === "ObjectProperty" && x.key?.name);
    const id = props.find((x) => x.key.name === "id");
    const kind = props.find((x) => x.key.name === "kind");
    if (id?.value?.type === "StringLiteral") out.set(id.value.value, kind?.value?.type === "StringLiteral" ? kind.value.value : null);
  });
  return out;
})();

const stateActions = (() => {
  const rows = roots.map((r) => (r.actions?.length
    ? { root: r, state: "NAMED", actions: r.actions, why: "declared at the source" }
    : { root: r, state: "UNDECLARED", actions: [], why: "the product declares no action here" }));
  const byAction = new Map();
  for (const x of rows) {
    for (const id of x.actions) {
      if (!byAction.has(id)) byAction.set(id, []);
      byAction.get(id).push(x.root);
    }
  }
  // The join R2 performs, one level up. A root's verdict is never rewritten by
  // the action it belongs to: a staged action is mutating because one of its
  // roots is, and the click that opens it stays read-only.
  const verdictOf = (r) => stateR2.find((x) => x.root === r)?.verdict ?? "UNRESOLVED";
  const actions = [...byAction.entries()].map(([id, rs]) => {
    const vs = rs.map(verdictOf);
    return { id, roots: rs, verdicts: vs,
      effect: vs.includes("MUTATION") ? "MUTATION" : vs.includes("UNRESOLVED") ? "UNRESOLVED" : "READ_ONLY",
      mixed: new Set(vs).size > 1,
      // The way a person performs it, not the mechanism that registered it:
      // a click and a keystroke are two modalities whether they arrive as two
      // JSX attributes or as an attribute and a listener.
      modalities: new Set(rs.map((r) => (r.kind === "command-chord" ? "chord" : r.event ?? "?"))),
      files: new Set(rs.map((r) => r.path)) };
  });
  return { rows, actions, forwarded: 0 };
})();

export { REGISTRY, stateActions };
