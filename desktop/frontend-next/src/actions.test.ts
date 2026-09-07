import { describe, expect, it } from "vitest";
import { ACTIONS } from "./actions";
import { certifyRoots } from "./ui/roots";

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
const resolve = (from: string, spec: string) => {
  if (!spec.startsWith(".")) return null;
  const parts = from.split("/").slice(0, -1).concat(spec.split("/"));
  const out: string[] = [];
  for (const p of parts) {
    if (p === "." || p === "") continue;
    if (p === "..") out.pop();
    else out.push(p);
  }
  const base = out.join("/");
  return [base + ".ts", base + ".tsx", base + "/index.ts", base + "/index.tsx"].find((c) => sources.has(c)) ?? null;
};
// The controls whose own internals are not entry points: a person acts on the
// call site that hands them a handler.
const PRIMITIVES = new Set(["src/ui/Switch.tsx", "src/ui/Menu.tsx"]);
const census = certifyRoots(sources, { skip: (p) => PRIMITIVES.has(p), resolve });

const registered = new Set(ACTIONS.map((a) => a.id));
const rendered = new Set(census.roots.flatMap((r) => r.actions));
const undeclared = census.roots.filter((r) => !r.actions.length);

// Inputs the product has not named. It may fall and never rise — and it is
// counted over every kind of input, not over the ones that happen to be
// buttons. Lower it when it drops; a rise means an input was added without a
// name, which is exactly what this is for.
const UNDECLARED_CEILING = 270;

/** One file, for holding the discovery rules themselves. */
const only = (src: string) => certifyRoots(new Map([["src/ui/X.tsx", src]]), { resolve });

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
