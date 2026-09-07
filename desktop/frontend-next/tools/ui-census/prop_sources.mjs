// Which attribute at a render site actually provides a prop.
//
// JSX is last-write: `<C cb={x} {...p} />` and `<C {...p} cb={x} />` do not
// mean the same thing, and a reader that collects attributes into a map and
// drops the spreads answers the first one with `x` whether or not `p` carries
// a `cb`. Today that answer happens to be right in this tree — the one object
// ever spread has thirteen keys and none of them collides — which is luck, not
// proof, and this is what turns it into proof.
//
// The question is per slot, never per spread: the same object answers ABSENT
// for one prop and SOURCES for another, so there is no such thing as a spread
// being "known".
import { localsOf, renders } from "./sites.mjs";

// What a spread argument carries, when that can be said exactly. An object
// literal whose keys are all plain names, or an identifier bound to one. Any
// other shape — a call, a conditional, a nested spread, a computed key — is
// not answered: an unknown spread may carry the slot, and guessing it does not
// is how a prop that overrides another goes unnoticed.
const keysOf = (node, path) => {
  let n = node;
  if (n?.type === "Identifier") n = localsOf(path).get(n.name) ?? null;
  if (n?.type !== "ObjectExpression") return null;
  const keys = new Map();
  for (const pr of n.properties ?? []) {
    if (pr.type !== "ObjectProperty" || pr.computed || pr.key?.type !== "Identifier") return null;
    keys.set(pr.key.name, pr.value);
  }
  return keys;
};

/** What one spread says about one slot. */
const slotFact = (spread, path, slot) => {
  const keys = keysOf(spread, path);
  if (!keys) return { status: "UNRESOLVED" };
  return keys.has(slot) ? { status: "SOURCES", sources: [keys.get(slot)] } : { status: "ABSENT" };
};

/** The effective source of one slot at one render site. Right to left, because
 *  the last attribute that can write the slot is the one that does; the scan
 *  stops at the first attribute that provides it and at the first spread that
 *  cannot be shown not to. */
function effectivePropSource(site, slot) {
  for (let i = (site.attrs ?? []).length - 1; i >= 0; i--) {
    const a = site.attrs[i];
    if (!a.spread) {
      if (a.name !== slot || !a.value) continue;
      return { status: "SOURCES", sources: [a.value], via: "attribute" };
    }
    const f = slotFact(a.spread, site.file, slot);
    if (f.status === "ABSENT") continue;
    return { ...f, via: "spread" };
  }
  return { status: "ABSENT" };
}

/** Where the scan answered with an explicit attribute, no spread to its right
 *  may be one this pass has not shown absent for that slot. True by
 *  construction, and checked because the construction is the thing at risk:
 *  collecting attributes into a map and dropping the spreads is the shape this
 *  reader had for as long as it existed, and it reads the same until a spread
 *  finally carries a colliding key. */
const shadowedByLaterSpread = [];
for (const [id, list] of renders) {
  for (const site of list) {
    const attrs = site.attrs ?? [];
    for (let i = 0; i < attrs.length; i++) {
      const slot = attrs[i].name;
      if (attrs[i].spread || !slot) continue;
      const eff = effectivePropSource(site, slot);
      if (eff.via !== "attribute" || eff.sources?.[0] !== attrs[i].value) continue;
      for (let j = i + 1; j < attrs.length; j++) {
        if (!attrs[j].spread) continue;
        if (slotFact(attrs[j].spread, site.file, slot).status === "ABSENT") continue;
        shadowedByLaterSpread.push(id + "  " + site.file + ":" + site.line + "  " + slot);
        break;
      }
    }
  }
}

export { effectivePropSource, keysOf, shadowedByLaterSpread, slotFact };
