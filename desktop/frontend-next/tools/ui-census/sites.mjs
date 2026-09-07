// Where a handler is written, and which component body encloses it.
import { join, dirname, resolve as resolvePath } from "node:path";
import { SRC, importsOf, jsxName, trees, walk, walkStack } from "./source.mjs";
import { ownerOf } from "./symbols.mjs";
import { bodyOf } from "./component_identity.mjs";

const HOPS = 24;
const ROLES = new Set(["tab", "option", "radio", "switch", "menuitem", "checkbox", "button", "menuitemradio", "treeitem"]);
const CTL = new Set(["Switch", "Menu"]);
// Declared against the tree being scanned, not against one working directory.
// As two literals these matched only when the census ran on `src` from the
// package root: scanning a copy elsewhere silently counted the primitives' own
// handlers as three extra interaction roots.
const PRIMITIVES = new Set(["Switch.tsx", "Menu.tsx"].map((f) => join(SRC, "ui", f)));
// Callees whose implementation is outside the module graph: the platform. Not
// a list of helpers believed pure — those are decided by the fixpoint.
const PLATFORM = /^(String|Number|Boolean|Object|Array|JSON|Math|Date|Promise|Set|Map|console|window|document|navigator|localStorage|sessionStorage|requestAnimationFrame|cancelAnimationFrame|setTimeout|clearTimeout|setInterval|clearInterval|structuredClone|encodeURIComponent|decodeURIComponent|alert|confirm|prompt|matchMedia|getComputedStyle|URL|Blob|FileReader|IntersectionObserver|ResizeObserver|MutationObserver|AbortController|crypto|performance|queueMicrotask|isNaN|parseInt|parseFloat)$/;


const renders = new Map();
const declares = new Map();
for (const [path, tree] of trees) {
  const names = new Set();
  walkStack(tree, (n, stack) => {
    if (n.type === "FunctionDeclaration" && n.id && /^[A-Z]/.test(n.id.name)) names.add(n.id.name);
    if (n.type === "VariableDeclarator" && n.id?.type === "Identifier" && /^[A-Z]/.test(n.id.name)) names.add(n.id.name);
    if (n.type !== "JSXOpeningElement") return;
    const name = jsxName(n.name);
    if (!/^[A-Z]/.test(name)) return;
    const props = new Map();
    // Spreads are recorded, not resolved. What a spread carries is the object's
    // question and a later attribute overrides it, so a site that has one is a
    // site whose prop set is not readable from the attributes alone — and the
    // order matters, because `{...p} cb={x}` and `cb={x} {...p}` do not mean
    // the same thing.
    const spreads = [];
    for (const a of n.attributes) {
      if (a.type === "JSXAttribute" && a.value) props.set(a.name.name, a.value);
      else if (a.type === "JSXSpreadAttribute") spreads.push({ node: a.argument, index: n.attributes.indexOf(a) });
    }
    const order = n.attributes.map((a) => (a.type === "JSXSpreadAttribute" ? "...spread" : a.name?.name ?? "?"));
    // The attributes in the order they are written, because which of them
    // provides a prop is decided by that order: a spread to the right of an
    // explicit attribute may replace it, and a props map alone cannot say.
    const attrs = n.attributes.map((a) => (a.type === "JSXSpreadAttribute"
      ? { spread: a.argument }
      : { name: a.name?.name ?? "?", value: a.value?.type === "JSXExpressionContainer" ? a.value.expression : a.value }));
    // A component's identity is where it is declared and what it is called
    // there, not what this file calls it. Twelve names in this tree are used by
    // two files each — Row, Bar, Node, Files — and a global name index followed
    // a prop from one into another; `import { Shell as ShellPicker }` is the
    // other direction, where the local spelling names nothing that exists.
    const imp = importsOf.get(path)?.get(name);
    // Indexed by the body, because that is what a reader asks about: a prop is
    // received where the component is written, and `memo(PaneView)` means every
    // <Pane> site is a render of PaneView. The alias is a certified render
    // target of a body, never a name a body may be confused with — two files
    // spelling a component Row are still two components.
    const id = bodyOf((imp?.file ?? path) + "#" + (imp?.name ?? name));
    if (!renders.has(id)) renders.set(id, []);
    renders.get(id).push({ file: path, line: n.loc.start.line, host: ownerOf(stack), props, spreads, order, attrs });
  });
  declares.set(path, names);
}
const localsCache = new Map();
const localsOf = (path) => {
  if (localsCache.has(path)) return localsCache.get(path);
  const m = new Map();
  walk(trees.get(path), (n) => {
    if (n.type === "VariableDeclarator" && n.id?.type === "Identifier" && n.init) m.set(n.id.name, n.init);
    if (n.type === "FunctionDeclaration" && n.id) m.set(n.id.name, n.body);
  });
  localsCache.set(path, m);
  return m;
};

export { CTL, HOPS, PLATFORM, PRIMITIVES, ROLES, declares, localsCache, localsOf, renders };
