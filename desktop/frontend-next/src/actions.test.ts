import { describe, expect, it } from "vitest";
import { ACTIONS } from "./actions";
import { PRIMITIVES, certifyRoots } from "./ui/roots";
import { parse } from "@babel/parser";

// What Studio says a person can do, held to what it actually renders. The
// registry is the oracle and production writes the same id again as a literal:
// two spellings that can disagree, which is the only reason this check is worth
// running. Importing one constant into both sides would compare a value with
// itself.
//
// The universe this runs over is not this file's to define. It comes from the
// same discovery the effect census uses, because the two disagreed once and the
// disagreement was invisible: this gate looked at onClick and onDoubleClick and
// therefore never saw 63 onChange, 16 onKeyDown or a single DOM listener —
// about a quarter of the product's inputs, including ones that write kernel
// state. A ceiling measured against that set could only ever fall for the
// wrong reason.
const SOURCES = import.meta.glob(["./**/*.{ts,tsx}", "!./**/*.test.*", "!./actions.ts"], {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

const sources = new Map(Object.entries(SOURCES).map(([key, src]) => ["src/" + key.slice(2), src]));
// The census tool's own sources, for the one rule that is about it.
const TOOLS = import.meta.glob(["../tools/ui-census/*.mjs"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const resolve = (from: string, spec: string, over: Map<string, string> = sources) => {
  if (!spec.startsWith(".")) return null;
  const parts = from.split("/").slice(0, -1).concat(spec.split("/"));
  const out: string[] = [];
  for (const p of parts) {
    if (p === "." || p === "") continue;
    if (p === "..") out.pop();
    else out.push(p);
  }
  const base = out.join("/");
  return [base + ".ts", base + ".tsx", base + "/index.ts", base + "/index.tsx"].find((c) => over.has(c)) ?? null;
};
// Which components are interaction primitives is src/ui/primitives.ts's to
// say, and this passes nothing: a second list here is the drift that broke a
// seal, and the way to make it impossible is to have nowhere to write one.
const census = certifyRoots(sources, { resolve });

const registered = new Set(ACTIONS.map((a) => a.id));
const rendered = new Set(census.roots.flatMap((r) => r.actions));
const undeclared = census.roots.filter((r) => !r.actions.length);

// Inputs the product has not named. It may fall and never rise — and it is
// counted over every kind of input, not over the ones that happen to be
// buttons. Lower it when it drops; a rise means an input was added without a
// name, which is exactly what this is for.
//
// It rose once, on purpose: certifying a primitive's callbacks brought the
// Picker call sites into the universe, and they had been outside it. A
// denominator that grows because the instrument was corrected is not the same
// event as one that grows because debt was added, and numbers either side of
// that change do not compare.
const UNDECLARED_CEILING = 219;

/** One file, for holding the discovery rules themselves. */
const only = (src: string) => certifyRoots(new Map([["src/ui/X.tsx", src]]), { resolve });
/** A fixture universe of several files, for the rules about what a call site
 *  resolves to. The primitive named here is the real one: its contract is what
 *  is under test, so restating it would test the restatement.
 *
 *  It resolves against its own files and not the product's. Sharing the outer
 *  resolver made a fixture component that does not exist in src resolve to
 *  nothing, so a test about ordinary components was quietly a test about
 *  unresolved ones and stayed green through a sabotage that promoted every
 *  component in the tree. */
const among = (files: Record<string, string>) => {
  const map = new Map(Object.entries(files));
  return certifyRoots(map, { resolve: (from, spec) => resolve(from, spec, map) });
};
const STUB = "export function Picker() { return null }";

describe("the actions Studio says a person can perform", () => {
  it("names every action it renders", () => {
    const missing = census.roots
      .flatMap((r) => r.actions.map((id) => ({ r, id })))
      .filter(({ id }) => !registered.has(id))
      .map(({ r, id }) => `${r.path}:${r.line}  ${id}`);
    expect(missing, "rendered but not in src/actions.ts").toEqual([]);
  });

  it("renders every action it names", () => {
    const stale = ACTIONS.map((a) => a.id).filter((id) => !rendered.has(id));
    expect(stale, "in src/actions.ts but nothing renders it — delete the entry or the debt hides here").toEqual([]);
  });

  it("refuses a declaration that cannot say which input it names", () => {
    const bad = census.refused.map((n) => `${n.path}:${n.line}  ${n.why}  ${n.detail ?? ""}`);
    expect(bad, "an element with several handlers needs data-action-<event>, and an action on a non-input is an error").toEqual([]);
  });

  it("places every registration it finds", () => {
    const open = census.uncertified.map((n) => `${n.path}:${n.line}  ${n.why}`);
    expect(open, "a registration neither certified as user input nor stated to be something else").toEqual([]);
  });

  it("spells an id as one surface and one intent, in neither locale", () => {
    const shape = /^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$/;
    expect(ACTIONS.map((a) => a.id).filter((id) => !shape.test(id))).toEqual([]);
    expect(ACTIONS.length).toBe(new Set(ACTIONS.map((a) => a.id)).size);
  });

  it("uses kinds that are all reachable", () => {
    for (const a of ACTIONS) expect(typeof a.kind).toBe("string");
    expect(new Set(ACTIONS.map((a) => a.kind)).size).toBeGreaterThan(3);
  });

  it("keeps the unnamed surface shrinking", () => {
    expect(
      undeclared.length,
      `${census.roots.length} inputs, ${rendered.size} actions, ${undeclared.length} unnamed. Lower UNDECLARED_CEILING when this drops`,
    ).toBeLessThanOrEqual(UNDECLARED_CEILING);
  });

  // If production imported the registry, the two sides would be one side.
  it("keeps the oracle out of the code it is checking", () => {
    const importers = Object.entries(SOURCES)
      .filter(([, src]) => /from\s+["'][^"']*\/actions["']|from\s+["']\.\/actions["']/.test(src))
      .map(([key]) => "src/" + key.slice(2));
    expect(importers, "production must write the id again, not import it").toEqual([]);
  });

  it("reads the sources it is meant to be guarding", () => {
    expect(sources.size).toBeGreaterThan(80);
    expect(census.roots.length).toBeGreaterThan(300);
    for (const kind of ["jsx-handler", "dom-event", "command-chord"]) {
      expect(census.roots.some((r) => r.kind === kind), `no ${kind} in the universe`).toBe(true);
    }
  });
});

describe("what counts as an input a person performs", () => {
  it("sees a handler that is not a click", () => {
    const c = only(`export const A = () => <input onChange={() => {}} />;`);
    expect(c.roots.map((r) => r.event)).toEqual(["change"]);
  });

  it("sees a listener as well as an attribute", () => {
    const c = only(`export function A() { window.addEventListener("keydown", () => {}); return null; }`);
    expect(c.roots.map((r) => r.kind)).toEqual(["dom-event"]);
  });

  it("leaves the environment's own events out", () => {
    const c = only(`export function A() { window.addEventListener("resize", () => {}); return null; }`);
    expect(c.roots).toEqual([]);
    expect(c.nonUser.map((n) => n.why)).toEqual(["RUNTIME_EVENT"]);
  });

  it("refuses one action on an element with two handlers", () => {
    const c = only(`export const A = () => <input data-action="a.b" onChange={() => {}} onKeyDown={() => {}} />;`);
    expect(c.refused.map((n) => n.why)).toEqual(["AMBIGUOUS_ELEMENT_ACTION"]);
    expect(c.roots.flatMap((r) => r.actions)).toEqual([]);
  });

  it("names the one handler a scoped declaration is written for", () => {
    const c = only(`export const A = () => <input data-action-keydown="a.b" onChange={() => {}} onKeyDown={() => {}} />;`);
    expect(c.roots.map((r) => `${r.event}:${r.actions.join()}`).sort()).toEqual(["change:", "keydown:a.b"]);
    expect(c.refused).toEqual([]);
  });
});

// A primitive hands a person's interaction out through a callback, and which
// callbacks those are is the primitive's to declare. Reading it off the prop's
// name instead is what left every Picker call site outside the universe — with
// a kernel write among them — while Seal A read as held.
describe("what a certified primitive's call site counts as", () => {
  it("makes the call site the input, on the contract rather than the spelling", () => {
    const c = among({
      "src/ui/X.tsx": `import { Picker } from "./Menu";
        export const A = () => <Picker data-action="a.b" onPick={(v) => save(v)} />;`,
      "src/ui/Menu.tsx": STUB,
    });
    expect(c.roots.map((r) => `${r.kind}:${r.event}:${r.prop}`)).toEqual(["jsx-handler:click:onPick"]);
    expect(c.roots[0].actions).toEqual(["a.b"]);
    expect(c.refused).toEqual([]);
  });

  it("leaves an ordinary component alone when its prop is spelled the same", () => {
    const c = among({
      "src/ui/X.tsx": `import { Foo } from "./Foo";
        export const A = () => <Foo data-action="a.b" onPick={(v) => save(v)} />;`,
      "src/ui/Foo.tsx": "export function Foo() { return null }",
    });
    // Not an input here: whatever Foo does with that callback, the thing a
    // person performs is inside Foo and is counted there.
    expect(c.roots).toEqual([]);
    expect(c.uncertified).toEqual([]);
  });

  it("counts only the callbacks the primitive declares", () => {
    const c = among({
      "src/ui/X.tsx": `import { Picker } from "./Menu";
        export const A = () => <Picker onHover={(v) => peek(v)} />;`,
      "src/ui/Menu.tsx": STUB,
    });
    expect(c.roots).toEqual([]);
  });

  // The destructive control. Without it the three above would still pass on an
  // implementation that promoted every component prop it liked the look of.
  it("rests on that declaration: production loses those roots without it", () => {
    const blind = certifyRoots(sources, {
      resolve,
      primitives: PRIMITIVES.map((p) => (p.file === "ui/Menu.tsx" ? { ...p, callbacks: {} } : p)),
    });
    const picks = (c: { roots: { prop?: string; actions: string[] }[] }) => c.roots.filter((r) => r.prop === "onPick");
    expect(picks(census).length).toBeGreaterThan(0);
    expect(picks(blind)).toEqual([]);
    // Including the one that switches the model, which is a kernel write and
    // was outside the universe until the contract was declared.
    expect(picks(census).some((r) => r.actions.includes("model.select"))).toBe(true);
  });
});

// The drift that broke a seal: the same fact written down twice, and only one
// copy updated. There is now one declaration and no way to pass another, so
// what this holds is that the census tool never starts keeping its own again.
describe("who decides which components are primitives", () => {
  it("is not the census tool: it passes certifyRoots nothing but a resolver", () => {
    const src = TOOLS["../tools/ui-census/certified.mjs"];
    expect(src, "certified.mjs is not being read").toBeTruthy();
    const tree = parse(src, { sourceType: "module" }) as unknown as { program: unknown };
    const keys: string[] = [];
    const walkAny = (n: unknown) => {
      if (!n || typeof (n as { type?: string }).type !== "string") return;
      const node = n as Record<string, unknown> & { type: string };
      if (node.type === "CallExpression" && (node.callee as { name?: string })?.name === "certifyRoots") {
        const opts = (node.arguments as Record<string, unknown>[])[1];
        for (const prop of ((opts?.properties as Record<string, unknown>[]) ?? [])) {
          keys.push(((prop.key as { name?: string })?.name) ?? "<computed>");
        }
      }
      for (const [k, v] of Object.entries(node)) {
        if (k === "loc") continue;
        if (Array.isArray(v)) v.forEach(walkAny);
        else walkAny(v);
      }
    };
    walkAny(tree.program);
    expect(keys, "the tool decides which files are primitives again").toEqual(["resolve"]);
  });
});
