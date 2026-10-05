import { describe, expect, it } from "vitest";
import { parse } from "@babel/parser";
import { ACTIONS } from "../actions";
import { walk, type Node } from "./roots";

// An action names what a person is doing. It is not the control they reached
// for, and it is not the surface they reached it from — the same intent has to
// keep the same identity when either of those changes, or the census is
// counting widgets instead of capabilities.
const SOURCE = Object.values(
  import.meta.glob("./Appearance.tsx", { query: "?raw", import: "default", eager: true }) as Record<string, string>,
)[0];

const tree = parse(SOURCE, { sourceType: "module", plugins: ["typescript", "jsx"], errorRecovery: true }) as unknown as Node;

interface Site { element: string; action: string; value: string | null; target: boolean }

const sites: Site[] = [];
walk(tree, (n) => {
  if (n.type !== "JSXOpeningElement") return;
  const attrs = ((n.attributes as Node[]) ?? []).filter((a) => a.type === "JSXAttribute");
  const at = (name: string) => attrs.find((a) => ((a.name as Node)?.name as string) === name);
  const lit = (a?: Node) => {
    const v = a?.value as Node | undefined;
    return v?.type === "StringLiteral" ? (v.value as string) : v ? "<expr>" : null;
  };
  const action = lit(at("data-action"));
  if (!action?.startsWith("appearance.")) return;
  const el = n.name as Node;
  sites.push({
    element: (el?.name as string) ?? "?",
    action,
    value: lit(at("data-value")),
    target: !!at("data-target"),
  });
});

const idsOf = (a: string) => sites.filter((s) => s.action === a);

describe("what the appearance page says a person is doing", () => {
  it("names fewer things than it renders controls for", () => {
    const ids = new Set(sites.map((s) => s.action));
    expect(sites.length).toBeGreaterThan(0);
    // If these were equal the vocabulary would be a rename of the DOM.
    expect(ids.size).toBeLessThan(sites.length);
  });

  // Four sliders editing four fields of one background, not four capabilities.
  it("treats the background as one thing with parameters", () => {
    const bg = idsOf("appearance.background");
    expect(bg.length).toBe(4);
    expect(new Set(bg.map((s) => s.value)).size).toBe(4);
    expect(bg.every((s) => s.value && s.value !== "<expr>")).toBe(true);
    // And no parameter got promoted to an id of its own.
    expect([...new Set(sites.map((s) => s.action))].filter((a) => a.startsWith("appearance.background."))).toEqual([]);
  });

  // The interface scale has a row of presets and a slider beside it. Two
  // element types, one intent — which is the whole claim: swapping the control
  // must not move the identity.
  it("keeps one intent across two kinds of control", () => {
    const zoom = idsOf("appearance.zoom");
    expect(zoom.length).toBeGreaterThan(1);
    expect(new Set(zoom.map((s) => s.element)).size).toBeGreaterThan(1);
  });

  // Which font slot is an entity, not a second capability.
  it("says which font by target rather than by inventing an id", () => {
    const font = idsOf("appearance.font");
    expect(font.length).toBeGreaterThan(1);
    expect(font.every((s) => s.target)).toBe(true);
    expect([...new Set(sites.map((s) => s.action))].filter((a) => /^appearance\.font\./.test(a))).toEqual([]);
  });

  // Alike in CSS is not alike as a thing to do. Installing a palette pack is
  // not choosing light or dark; deciding which image is behind the window is
  // not deciding how that image is drawn.
  it("keeps neighbouring intents apart", () => {
    const ids = new Set(ACTIONS.map((a) => a.id));
    for (const id of ["theme.activate", "wallpaper.change", "wallpaper.remove", "appearance.scheme", "appearance.background",
      "appearance.weight", "appearance.contrast"]) {
      expect(ids.has(id), `${id} is missing`).toBe(true);
    }
  });

  // The settings sheet is where these are reached today. It is not what they
  // are, and an id that said so would need a second one the day the command
  // palette or onboarding can change the same thing.
  it("names the setting and not the surface it is reached from", () => {
    const surfaces = /(^|\.)(settings|prefs|panel|sheet|dialog|page)(\.|$)/;
    expect(ACTIONS.map((a) => a.id).filter((id) => id.startsWith("appearance.") && surfaces.test(id))).toEqual([]);
  });
});
