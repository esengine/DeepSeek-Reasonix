import { describe, expect, it } from "vitest";
import { parse } from "@babel/parser";

// A refusal the kernel gave an identity is worth nothing if the last hop prints
// the English that rode along for the log. reason() is that hop — it says a
// coded refusal in the reader's language, and keeps a dead process from putting
// a path and a status on screen. Reading .message off the caught error walks
// straight past it, which twenty-four sites in the panels used to do.
//
// Vite reads the sources; the frontend carries no Node types, so a check that
// needed them would only typecheck by accident.
const SOURCES = import.meta.glob(["./**/*.{ts,tsx}", "!./**/*.test.*"], {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

// What counts as an exception: the binding of a catch clause, the single
// parameter of a .catch() callback, and the rejection half of .then(ok, err).
// Nothing else is known to be an error, and a rule that guessed from a name
// would be reading spelling again.
//
// Deliberately not covered, so nobody mistakes this for the whole boundary: an
// error handed to a function and read there, and a rejection reaching a handler
// declared elsewhere. Those need types this walk does not have.
type Node = { type: string; loc?: { start: { line: number } } } & Record<string, unknown>;

function walk(node: unknown, fn: (n: Node) => void) {
  if (!node || typeof (node as Node).type !== "string") return;
  const n = node as Node;
  fn(n);
  for (const [key, value] of Object.entries(n)) {
    if (key === "loc" || key.endsWith("Comments")) continue;
    if (Array.isArray(value)) value.forEach((c) => walk(c, fn));
    else walk(value, fn);
  }
}

const at = (n: Node) => n.loc?.start.line ?? 0;
const named = (v: unknown, name: string) => !!v && (v as Node).type === "Identifier" && (v as { name: string }).name === name;

/** Exception bindings in one file, each with the body its name is live in. */
function exceptions(ast: unknown): { name: string; body: unknown }[] {
  const out: { name: string; body: unknown }[] = [];
  const fromCallback = (fn: unknown, body: unknown[]) => {
    const f = fn as Node | undefined;
    if (!f || (f.type !== "ArrowFunctionExpression" && f.type !== "FunctionExpression")) return;
    const params = f.params as Node[];
    if (params.length !== 1 || params[0].type !== "Identifier") return;
    body.push({ name: (params[0] as unknown as { name: string }).name, body: f.body });
  };
  walk(ast, (n) => {
    if (n.type === "CatchClause" && (n.param as Node | null)?.type === "Identifier") {
      out.push({ name: (n.param as unknown as { name: string }).name, body: n.body });
    }
    if (n.type !== "CallExpression") return;
    const callee = n.callee as Node | undefined;
    if (!callee || callee.type !== "MemberExpression" || callee.computed) return;
    const method = (callee.property as unknown as { name?: string })?.name;
    const args = n.arguments as unknown[];
    if (method === "catch") fromCallback(args[0], out);
    if (method === "then" && args.length === 2) fromCallback(args[1], out);
  });
  return out;
}

/** Whether this node sits inside a call the developer alone reads. Named, not
 *  inferred: a sink is a diagnostic because someone said so. */
const DEV_SINKS = ["console"];
function underDevSink(ast: unknown, target: Node): boolean {
  let found = false;
  walk(ast, (n) => {
    if (n.type !== "CallExpression") return;
    const callee = n.callee as Node | undefined;
    if (!callee || callee.type !== "MemberExpression") return;
    if (!DEV_SINKS.some((s) => named(callee.object, s))) return;
    walk(n, (inner) => {
      if (inner === target) found = true;
    });
  });
  return found;
}

/** Lines in one source where a caught error's own message is read. */
function rawReads(source: string): number[] {
  const ast = parse(source, { sourceType: "module", plugins: ["typescript", "jsx"], errorRecovery: true });
  const out: number[] = [];
  for (const exc of exceptions(ast)) {
    walk(exc.body, (n) => {
      if (n.type !== "MemberExpression" || n.computed) return;
      if (!named(n.object, exc.name) || !named(n.property, "message")) return;
      if (underDevSink(ast, n)) return;
      out.push(at(n));
    });
  }
  // catch ({ message }) is the same read wearing a pattern.
  walk(ast, (n) => {
    if (n.type !== "CatchClause" || (n.param as Node | null)?.type !== "ObjectPattern") return;
    out.push(at(n));
  });
  return out.sort((a, b) => a - b);
}

describe("a refusal on its way to the reader", () => {
  it("goes through reason(), never through the caught error's own message", () => {
    const raw: string[] = [];
    for (const [key, body] of Object.entries(SOURCES)) {
      for (const line of rawReads(body)) raw.push(`src/${key.slice(2)}:${line}`);
    }
    expect(raw, "print reason(e), not e.message — see src/i18n/kernel.ts").toEqual([]);
  });

  it("reads the sources it is meant to be guarding", () => {
    expect(Object.keys(SOURCES).length).toBeGreaterThan(80);
    // The walk has to actually find exception bindings, or an empty result is
    // parity by accident. The panels are full of them.
    const found = Object.values(SOURCES).reduce(
      (n, body) =>
        n + exceptions(parse(body, { sourceType: "module", plugins: ["typescript", "jsx"], errorRecovery: true })).length,
      0,
    );
    expect(found).toBeGreaterThan(40);
  });

  // Each shape, because the tree happens to hold one of them today and the next
  // one written will be whichever this did not look for.
  it("finds the read whichever way the error was bound", () => {
    expect(rawReads(`try { go() } catch (e) { setError(e.message) }`)).toEqual([1]);
    expect(rawReads(`go().catch((e) => setError(e.message))`)).toEqual([1]);
    expect(rawReads(`go().then(ok, (e) => setError(e.message))`)).toEqual([1]);
    expect(rawReads(`try { go() } catch ({ message }) { setError(message) }`)).toEqual([1]);
    expect(rawReads(`try { go() } catch (e) { setError(e instanceof Error ? e.message : String(e)) }`)).toEqual([1]);
  });

  it("leaves alone what is not an error, and what only a developer reads", () => {
    // A field that happens to be spelled the same on something that was never
    // thrown. Reading names is what this must not start doing.
    expect(rawReads(`const shown = props.message; render(shown)`)).toEqual([]);
    expect(rawReads(`try { go() } catch (e) { setError(reason(e)) }`)).toEqual([]);
    expect(rawReads(`try { go() } catch (e) { console.warn(e.message) }`)).toEqual([]);
    // One argument, so nothing there is a rejection handler.
    expect(rawReads(`go().then((r) => setError(r.message))`)).toEqual([]);
  });
});
