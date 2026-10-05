import { describe, expect, it } from "vitest";
import { parse } from "@babel/parser";
import { walk, type Node } from "./roots";

// A write needs a gesture someone made on purpose.
//
// Blur is not one: it fires when focus moves, for reasons nobody aimed at —
// a click elsewhere, a Tab, a window switching away, an element being
// removed. The census classifies it as a runtime event for that reason, so a
// field that saves on blur and nowhere else performs a host write that no
// interaction root can ever account for. Seal A then reads "every proven
// write has an identity" over a universe those writes are not in.
//
// Saving on blur is still allowed, and is often kind — clicking away is an
// answer. What is not allowed is blur being the only way in. Where one of
// these commits, the same commit has to be reachable from a real user input
// on the same control, and that input carries the identity.
const SOURCES = import.meta.glob(["./**/*.tsx", "!./**/*.test.*"], {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

const tree = (src: string) =>
  parse(src, { sourceType: "module", plugins: ["typescript", "jsx"], errorRecovery: true }) as unknown as Node;

/** Names an expression mentions, and the local functions it reaches through. */
function reaches(t: Node, from: Node, hops = 3): Set<string> {
  const locals = new Map<string, Node>();
  walk(t, (n) => {
    if (n.type === "FunctionDeclaration" && (n.id as Node)?.name) locals.set((n.id as Node).name as string, n);
    if (n.type === "VariableDeclarator" && (n.id as Node)?.type === "Identifier" && n.init) {
      locals.set((n.id as Node).name as string, n.init as Node);
    }
  });
  const seen = new Set<string>();
  const out = new Set<string>();
  const take = (node: Node, left: number) => {
    walk(node, (d) => {
      if (d.type === "Identifier") out.add(d.name as string);
      if (d.type === "MemberExpression" && (d.object as Node)?.type === "Identifier") {
        out.add(`${(d.object as Node).name as string}.${((d.property as Node)?.name as string) ?? "?"}`);
      }
    });
    if (left <= 0) return;
    for (const name of [...out]) {
      if (seen.has(name)) continue;
      seen.add(name);
      const body = locals.get(name);
      if (body) take(body, left - 1);
    }
  };
  take(from, hops);
  return out;
}

/** Whether what this reaches leaves the component: a port or hub call, or a
 *  callback handed in from outside. Local setState never does. */
const escapes = (names: Set<string>) =>
  [...names].some((n) => /^(port|hub)\./.test(n) || /^on[A-Z]/.test(n));

const offenders: string[] = [];
for (const [path, src] of Object.entries(SOURCES)) {
  const t = tree(src);
  walk(t, (n) => {
    if (n.type !== "JSXOpeningElement") return;
    const attrs = ((n.attributes as Node[]) ?? []).filter((a) => a.type === "JSXAttribute");
    const named = (a: Node) => ((a.name as Node)?.name as string) ?? "";
    const blur = attrs.find((a) => named(a) === "onBlur");
    if (!blur) return;
    const v = blur.value as Node | undefined;
    const body = v?.type === "JSXExpressionContainer" ? (v.expression as Node) : undefined;
    if (!body || !escapes(reaches(t, body))) return;
    // It commits. Something on this element has to be a user's aim.
    if (attrs.some((a) => named(a).startsWith("data-action"))) return;
    offenders.push(`${path}:${n.loc?.start.line}`);
  });
}

describe("a write that only happens on blur", () => {
  it("does not exist: every commit-on-blur has an aimed-at trigger beside it", () => {
    expect(offenders, "this field saves on blur and declares no user input that also saves").toEqual([]);
  });

  // The check has to be able to see one, or it is a clean sweep over nothing.
  it("would see one", () => {
    const found: string[] = [];
    const t = tree(`export const A = ({ onRename }: { onRename: (s: string) => void }) =>
      <input onBlur={(e) => onRename(e.currentTarget.value)} />;`);
    walk(t, (n) => {
      if (n.type !== "JSXOpeningElement") return;
      const attrs = ((n.attributes as Node[]) ?? []).filter((a) => a.type === "JSXAttribute");
      const blur = attrs.find((a) => (((a.name as Node)?.name as string) ?? "") === "onBlur");
      const v = blur?.value as Node | undefined;
      const body = v?.type === "JSXExpressionContainer" ? (v.expression as Node) : undefined;
      if (body && escapes(reaches(t, body))) found.push("yes");
    });
    expect(found).toEqual(["yes"]);
  });

  // And it must not call a field that only closes a menu on blur a commit.
  it("leaves local state alone", () => {
    const t = tree(`export const A = () => { const [, setOpen] = useState(false);
      return <input onBlur={() => setOpen(false)} />; };`);
    let flagged = false;
    walk(t, (n) => {
      if (n.type !== "JSXOpeningElement") return;
      const attrs = ((n.attributes as Node[]) ?? []).filter((a) => a.type === "JSXAttribute");
      const blur = attrs.find((a) => (((a.name as Node)?.name as string) ?? "") === "onBlur");
      const v = blur?.value as Node | undefined;
      const body = v?.type === "JSXExpressionContainer" ? (v.expression as Node) : undefined;
      if (body && escapes(reaches(t, body))) flagged = true;
    });
    expect(flagged).toBe(false);
  });
});
