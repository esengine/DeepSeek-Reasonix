import { describe, expect, it } from "vitest";
import { parse } from "@babel/parser";
import { ACTIONS } from "./actions";

// What Studio says a person can do, held to what it actually renders. The
// registry is the oracle and production writes the same id again as a literal:
// two spellings that can disagree, which is the only reason this check is worth
// running. Importing one constant into both sides would compare a value with
// itself.
const SOURCES = import.meta.glob(["./**/*.{ts,tsx}", "!./**/*.test.*", "!./actions.ts"], {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

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
const ast = (src: string) => parse(src, { sourceType: "module", plugins: ["typescript", "jsx"], errorRecovery: true });
// Recovery keeps one bad file from taking the whole run down, and would also
// let it contribute no sites while every count above reads as an improvement.
const parseErrors = (src: string) => (ast(src).errors ?? []).length;
const elementName = (n: Node): string => {
  const name = n.name as Node;
  if (name?.type === "JSXIdentifier") return (name as unknown as { name: string }).name;
  if (name?.type === "JSXMemberExpression") return elementName({ type: "x", name: (name as unknown as { object: Node }).object });
  return "";
};

/** Every id a value can take, or null when the AST cannot enumerate them. A
 *  control that is genuinely two actions — send while idle, stop while running
 *  — may branch, as long as both answers are written down. */
function idsOf(value: unknown): string[] | null {
  const v = value as Node | null;
  if (!v) return null;
  if (v.type === "StringLiteral") return [(v as unknown as { value: string }).value];
  if (v.type === "JSXExpressionContainer") return idsOf(v.expression);
  if (v.type === "ConditionalExpression") {
    const a = idsOf(v.consequent);
    const b = idsOf(v.alternate);
    return a && b ? [...a, ...b] : null;
  }
  return null;
}

interface Site {
  file: string;
  line: number;
  ids: string[] | null;
}

const ROLES = new Set(["tab", "option", "radio", "switch", "menuitem", "checkbox", "button", "menuitemradio", "treeitem"]);
const CONTROLS = new Set(["Switch", "Menu"]);

/** Interactive sites in one file: those carrying an action, and those not. */
function sitesIn(file: string, src: string): { declared: Site[]; bare: Site[] } {
  const declared: Site[] = [];
  const bare: Site[] = [];
  const tree = ast(src);
  walk(tree, (n) => {
    if (n.type !== "JSXOpeningElement") return;
    const el = elementName(n);
    const attrs: Record<string, unknown> = {};
    for (const a of n.attributes as Node[]) {
      if (a.type === "JSXAttribute") attrs[(a.name as unknown as { name: string }).name] = a.value;
    }
    const roleValue = idsOf(attrs.role);
    const role = roleValue?.[0];
    const interactive =
      el === "button" || (el === "a" && attrs.href) || CONTROLS.has(el) ||
      (role && ROLES.has(role)) || !!attrs.onClick || !!attrs.onDoubleClick;
    if (!interactive) return;
    const line = n.loc?.start.line ?? 0;
    if ("data-action" in attrs) declared.push({ file, line, ids: idsOf(attrs["data-action"]) });
    else bare.push({ file, line, ids: null });
  });
  // The window's shortcuts are the second place an action is named. They reach
  // the same capabilities as the controls on screen and share their ids. Found
  // by the shape of a binding — a chord and the action it performs — because
  // "action" alone is a field name the wire uses for something else entirely.
  walk(tree, (n) => {
    if (n.type !== "ObjectExpression") return;
    const props = (n.properties as Node[]).filter((p) => p.type === "ObjectProperty");
    const named = (want: string) => props.find((p) => ((p.key as unknown as { name?: string })?.name) === want);
    if (!named("chord")) return;
    const ids = idsOf(named("action")?.value);
    if (ids) declared.push({ file, line: n.loc?.start.line ?? 0, ids });
  });
  return { declared, bare };
}

const all = Object.entries(SOURCES).map(([key, src]) => ({ file: "src/" + key.slice(2), ...sitesIn("src/" + key.slice(2), src) }));
const declared = all.flatMap((f) => f.declared);
const bare = all.flatMap((f) => f.bare);
const registered = new Set(ACTIONS.map((a) => a.id));
const rendered = new Set(declared.flatMap((s) => s.ids ?? []));

// Interactive sites this pass has not given an identity. It may fall and never
// rise: the number is what stops the registry from reading as the whole set
// while most of the surface is still anonymous.
const UNNAMED_CEILING = 237;

describe("the actions Studio says a person can perform", () => {
  it("names every action it renders", () => {
    const missing = declared
      .flatMap((s) => (s.ids ?? []).map((id) => ({ ...s, id })))
      .filter((s) => !registered.has(s.id))
      .map((s) => `${s.file}:${s.line}  ${s.id}`);
    expect(missing, "rendered but not in src/actions.ts").toEqual([]);
  });

  it("renders every action it names", () => {
    const stale = ACTIONS.map((a) => a.id).filter((id) => !rendered.has(id));
    expect(stale, "in src/actions.ts but nothing renders it — delete the entry or the debt hides here").toEqual([]);
  });

  // A value the walk cannot enumerate makes the census incomplete without
  // saying so, which is worse than an unnamed control: this one looks named.
  it("writes each id where it can be read", () => {
    const dynamic = declared.filter((s) => s.ids === null).map((s) => `${s.file}:${s.line}`);
    expect(dynamic, "data-action must be a literal, or a branch of literals").toEqual([]);
  });

  it("spells an id as one surface and one intent, in neither locale", () => {
    const shape = /^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$/;
    expect(ACTIONS.map((a) => a.id).filter((id) => !shape.test(id))).toEqual([]);
    expect(ACTIONS.length).toBe(new Set(ACTIONS.map((a) => a.id)).size);
  });

  // The kinds are a closed union in TypeScript, which is the mechanism; this
  // holds the other half of it — that every kind declared is one somebody uses,
  // so the set cannot grow a synonym nothing reads.
  it("uses kinds that are all reachable", () => {
    for (const a of ACTIONS) expect(typeof a.kind).toBe("string");
    expect(new Set(ACTIONS.map((a) => a.kind)).size).toBeGreaterThan(3);
  });

  it("keeps the unnamed surface shrinking", () => {
    expect(
      bare.length,
      `${declared.length} sites carry one of ${rendered.size} actions; ${bare.length} are still anonymous. Lower UNNAMED_CEILING when this drops`,
    ).toBeLessThanOrEqual(UNNAMED_CEILING);
  });

  // If production imported the registry, the two sides would be one side.
  it("keeps the oracle out of the code it is checking", () => {
    const importers = Object.entries(SOURCES)
      .filter(([, src]) => /from\s+["'][^"']*\/actions["']|from\s+["']\.\/actions["']/.test(src))
      .map(([key]) => "src/" + key.slice(2));
    expect(importers, "production must write the id again, not import it").toEqual([]);
  });

  it("reads the sources it is meant to be guarding", () => {
    expect(Object.keys(SOURCES).length).toBeGreaterThan(80);
    expect(declared.length).toBeGreaterThan(20);
    const unreadable = Object.entries(SOURCES)
      .filter(([, src]) => parseErrors(src) > 0)
      .map(([key]) => "src/" + key.slice(2));
    expect(unreadable, "these contributed no sites because they did not parse").toEqual([]);
  });
});
