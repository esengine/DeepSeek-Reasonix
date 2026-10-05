import { describe, expect, it } from "vitest";
import { parse } from "@babel/parser";
import { PRIMITIVES, USER_INPUT, eventOfProp, walk, type Node } from "./roots";

// A declaration nobody checks is a claim. These hold each certified callback
// to the component that declares it: the prop has to be raised by a real DOM
// interaction in that file, and by one of the event it names. Without this the
// authority would be a place to write "onWhatever is a click" and have the
// census believe it.
const SOURCES = import.meta.glob(["./**/*.tsx"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const sources = new Map(Object.entries(SOURCES).map(([k, src]) => ["ui/" + k.slice(2), src]));

const tree = (src: string) =>
  parse(src, { sourceType: "module", plugins: ["typescript", "jsx"], errorRecovery: true }) as unknown as Node;

/** Every identifier named inside a node. The question is only which names a
 *  handler can reach, so a reference is enough — calling it is not required to
 *  hand it on. */
function namesIn(n: Node): Set<string> {
  const out = new Set<string>();
  walk(n, (d) => {
    if (d.type === "Identifier") out.add(d.name as string);
  });
  return out;
}

/** Functions this file declares, by the name they are reached through. */
function localsOf(t: Node): Map<string, Node> {
  const out = new Map<string, Node>();
  walk(t, (n) => {
    if (n.type === "FunctionDeclaration" && (n.id as Node)?.name) out.set((n.id as Node).name as string, n);
    if (n.type === "VariableDeclarator" && (n.id as Node)?.type === "Identifier" && n.init) {
      out.set((n.id as Node).name as string, n.init as Node);
    }
  });
  return out;
}

/** The JSX handler expressions in this file for one DOM event. */
function handlersFor(t: Node, event: string): Node[] {
  const out: Node[] = [];
  walk(t, (n) => {
    if (n.type !== "JSXAttribute") return;
    if (eventOfProp((n.name as Node)?.name as string) !== event) return;
    const v = n.value as Node | undefined;
    if (v) out.push(v.type === "JSXExpressionContainer" ? (v.expression as Node) : v);
  });
  return out;
}

// Four hops: a menu item's onClick calls take(), take calls the prop. Deeper
// than anything a primitive should need, and bounded so a cycle cannot spin.
const HOPS = 4;

function reaches(t: Node, from: Node[], want: string): boolean {
  const locals = localsOf(t);
  const seen = new Set<string>();
  let frontier = from.flatMap((n) => [...namesIn(n)]);
  for (let hop = 0; hop <= HOPS; hop++) {
    if (frontier.includes(want)) return true;
    const next: string[] = [];
    for (const name of frontier) {
      if (seen.has(name)) continue;
      seen.add(name);
      const body = locals.get(name);
      if (body) next.push(...namesIn(body));
    }
    if (next.length === 0) return false;
    frontier = next;
  }
  return false;
}

describe("the components certified as interaction primitives", () => {
  it("declares each one against a file that is really in the tree", () => {
    for (const p of PRIMITIVES) expect(sources.has(p.file), `${p.file} is declared and not present`).toBe(true);
  });

  it("names only events a person performs", () => {
    for (const p of PRIMITIVES) {
      for (const [prop, event] of Object.entries(p.callbacks)) {
        expect(USER_INPUT.has(event), `${p.file} ${prop} names ${event}`).toBe(true);
      }
    }
  });

  // The contract, held to the body. This is what makes onPick an interaction:
  // Menu really does raise it from a click, and the claim is checked rather
  // than read off the prop's spelling.
  it("raises every callback it declares, from the event it declares", () => {
    const unbacked: string[] = [];
    for (const p of PRIMITIVES) {
      const t = tree(sources.get(p.file)!);
      for (const [prop, event] of Object.entries(p.callbacks)) {
        const from = handlersFor(t, event);
        if (from.length === 0) unbacked.push(`${p.file} declares ${prop} on ${event} and has no ${event} handler`);
        else if (!reaches(t, from, prop)) unbacked.push(`${p.file}: no ${event} handler reaches ${prop}`);
      }
    }
    expect(unbacked, "a certified callback with nothing behind it").toEqual([]);
  });

  // The negative control for the one above: a claim about a prop the component
  // never hands on has to fail, or the check proves nothing.
  it("refuses a callback the body never raises", () => {
    const t = tree(sources.get("ui/Menu.tsx")!);
    expect(reaches(t, handlersFor(t, "click"), "onPick")).toBe(true);
    expect(reaches(t, handlersFor(t, "click"), "onNeverCalled")).toBe(false);
    // And an event the file has no handler for reaches nothing at all.
    expect(handlersFor(t, "drop")).toEqual([]);
  });
});
