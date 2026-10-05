// Effect facts: what a setup and a cleanup each reach, and on which triggers.
import { byNode as registrations } from "./registrations.mjs";
import { classify } from "./classify.mjs";
import { localsOf } from "./sites.mjs";
import { flat, isCall, productFiles, trees, walk, walkStack } from "./source.mjs";
import { ownerOf } from "./symbols.mjs";

const effectRows = (() => {
  // Setup and cleanup are different lifecycles with different triggers, so they
  // are measured apart. React calls the returned function before the next setup
  // and at unmount — that is a framework contract, not the general returned-
  // callable flow this analyzer still refuses.
  const facts = (cb, path, comp, registered) => {
    const out = { mutates: new Set(), open: [], stateWrites: new Set(), scheduled: new Set(),
      directMutations: new Set(), scheduledMutations: new Set() };
    if (cb) classify(cb, path, comp, 0, new Set(), out, [path], registered);
    // The count decides the verdict; the kinds decide what would close it, and
    // a cause discarded here is one every axis downstream reports as "open"
    // with nothing behind it.
    return { direct: [...out.directMutations], scheduled: [...out.scheduledMutations],
      open: out.open.length, openEdges: out.open };
  };
  // The function an effect body returns at its top level, if any. A concise
  // body IS that return — `() => () => stop()` is all cleanup and no setup, and
  // reading only block bodies filed its teardown as work the setup performs.
  const cleanupOf = (cb, path) => {
    if (!cb || !cb.body) return null;
    if (cb.body.type === "ArrowFunctionExpression" || cb.body.type === "FunctionExpression") return cb.body;
    if (cb.body.type !== "BlockStatement") return null;
    for (const st of cb.body.body) {
      if (st.type !== "ReturnStatement" || !st.argument) continue;
      const a = st.argument;
      if (a.type === "ArrowFunctionExpression" || a.type === "FunctionExpression") return a;
      if (a.type === "Identifier") return localsOf(path).get(a.name) ?? { unresolved: true };
      return { unresolved: true };
    }
    return null;
  };
  const depShape = (n) => {
    const d = n.arguments[1];
    if (!d) return { kind: "EVERY_COMMIT", names: [] };
    if (d.type !== "ArrayExpression") return { kind: "UNRESOLVED_DEPS", names: [] };
    if (d.elements.length === 0) return { kind: "MOUNT_ONLY", names: [] };
    const names = d.elements.map((e) => (e ? flat(e) : "?"));
    return { kind: names.includes("?") ? "UNRESOLVED_DEPS" : "EXPLICIT_DEPS", names };
  };

  const rows = [];
  for (const [path, tree] of trees) {
    if (!productFiles.has(path)) continue;
    walkStack(tree, (n, stack) => {
      if (!isCall(n) || !/(^|\.)(useEffect|useLayoutEffect|useInsertionEffect)$/.test(flat(n.callee))) return;
      const cb = n.arguments[0];
      if (!cb) return;
      const registered = new Set();
      let installs = false;
      // Installing an interaction is what makes an effect a registration site;
      // removing one is not, and neither runs the listener it names. Both
      // projections come from the one certified fact.
      walk(cb, (x) => {
        const reg = registrations.get(x);
        if (!reg) return;
        if (reg.operation === "ADD") installs = true;
        const arg = reg.listener;
        if (!arg) return;
        registered.add(arg);
        if (arg.type === "Identifier") registered.add("Local(" + path + "," + arg.name + ")");
      });
      const comp = ownerOf(stack);
      const cu = cleanupOf(cb, path);
      // What the setup itself runs, with the returned cleanup taken out of it.
      const body = cb.body?.type === "ArrowFunctionExpression" || cb.body?.type === "FunctionExpression"
        ? { ...cb, body: { type: "BlockStatement", body: [], directives: [], loc: cb.loc } }
        : cb;
      rows.push({
        path, line: n.loc.start.line,
        setup: facts(body, path, comp, registered), registers: installs,
        cleanup: !cu ? null : cu.unresolved ? { unresolved: true } : facts(cu, path, comp, registered),
        deps: depShape(n), comp,
      });
    });
  }
  return rows;
})();

export { effectRows };
