import { describe, expect, it } from "vitest";
import { parse } from "@babel/parser";
import { renderToStaticMarkup } from "react-dom/server";
import { Group } from "./Group";
import { SETTINGS, SETTING_AT, type SettingScope } from "./prefsnav";
import { walk, type Node } from "./roots";

// The settings screen has to answer two questions about itself: where a
// setting is, and what changing it costs. Both are read from one table, and
// this holds that table to what the screen really renders — in both
// directions, because a row nothing draws hides the same debt a block with no
// row does.
const SOURCES = import.meta.glob(["./**/*.tsx", "!./**/*.test.*"], {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>;

const tree = (src: string) =>
  parse(src, { sourceType: "module", plugins: ["typescript", "jsx"], errorRecovery: true }) as unknown as Node;

const attr = (el: Node, name: string): Node | undefined =>
  ((el.attributes as Node[]) ?? []).find((a) => a.type === "JSXAttribute" && ((a.name as Node)?.name as string) === name);

const literal = (a?: Node): string | null => {
  const v = a?.value as Node | undefined;
  return v?.type === "StringLiteral" ? (v.value as string) : null;
};

const name = (el: Node): string => {
  const n = el.name as Node;
  return n?.type === "JSXIdentifier" ? (n.name as string) : "";
};

/** Every settings block the tree renders, by the id it names itself with.
 *  A hand-drawn frame counts when its direct group header carries an h3 — the
 *  same structure Group renders, unlike nested labels such as Storage's. */
function rendered(): { ids: string[]; unnamed: string[] } {
  const ids: string[] = [];
  const unnamed: string[] = [];
  for (const [path, src] of Object.entries(SOURCES)) {
    walk(tree(src), (n) => {
      if (n.type === "JSXOpeningElement" && name(n) === "Group") {
        const id = literal(attr(n, "id"));
        if (id) ids.push(id);
        else unnamed.push(`${path}:${n.loc?.start.line}  <Group> with no literal id`);
        return;
      }
      if (n.type !== "JSXElement") return;
      const open = n.openingElement as Node;
      if (literal(attr(open, "className")) !== "grp") return;
      const titled = ((n.children as Node[]) ?? []).some((child) => {
        if (child.type !== "JSXElement") return false;
        const childOpen = child.openingElement as Node;
        if (literal(attr(childOpen, "className")) !== "grp-hd") return false;
        let heading = false;
        walk(child, (d) => {
          if (d.type === "JSXOpeningElement" && name(d) === "h3") heading = true;
        });
        return heading;
      });
      if (!titled) return;
      const said = attr(open, "data-setting");
      const id = literal(said);
      if (id) ids.push(id);
      // Group names itself from its prop, so the attribute is an expression
      // there and the ids come from its call sites. Absent is the failure.
      else if (!said) unnamed.push(`${path}:${open.loc?.start.line}  a titled block that names no id`);
    });
  }
  return { ids, unnamed };
}

describe("the settings catalogue", () => {
  const seen = rendered();

  it("carries every block the screen renders", () => {
    const known = new Set(SETTINGS.map((e) => e.anchor));
    const missing = [...new Set(seen.ids)].filter((id) => !known.has(id));
    expect(missing, "rendered with no row in SETTINGS — where is it, and what does changing it cost?").toEqual([]);
  });

  it("renders every block it carries", () => {
    const drawn = new Set(seen.ids);
    const stale = SETTINGS.map((e) => e.anchor).filter((a) => !drawn.has(a));
    expect(stale, "a row nothing renders: delete it, or the debt hides here").toEqual([]);
  });

  // Fail closed. A block with a heading is a setting, and a setting that does
  // not name itself has no apply semantics and cannot be found by search.
  it("leaves no titled block unnamed", () => {
    expect(seen.unnamed, "a titled settings block that names no id").toEqual([]);
  });

  it("gives every anchor one row", () => {
    // The screen may draw one anchor from two mutually exclusive branches —
    // the effort ladder has a form for an endpoint that declares no levels —
    // so it is the table that has to be unique, not the render sites.
    expect(SETTINGS.length).toBe(new Set(SETTINGS.map((e) => e.anchor)).size);
  });

  // Sabotage A. scope is required in the type, so an entry without one cannot
  // compile — but a type is not a gate a test can demonstrate, and a value
  // outside the taxonomy would pass typecheck if the union ever widened.
  it("gives every setting an ownership scope from the declared taxonomy", () => {
    const known = new Set<SettingScope>(["session", "model", "workspace", "machine", "account", "chosen"]);
    const bad = SETTINGS.filter((e) => !known.has(e.scope)).map((e) => `${e.anchor}: ${String(e.scope)}`);
    expect(bad, "a scope outside the taxonomy is a scope nobody audited").toEqual([]);
  });

  // Sabotage B. Information architecture must not become authority. Every one
  // of these pairs is a real finding from the audit, and each would be lost the
  // moment scope were derived from the page a block is filed under.
  it("does not let the section a block sits on decide who owns it", () => {
    const bySection = new Map<string, Set<SettingScope>>();
    for (const e of SETTINGS) bySection.set(e.section, (bySection.get(e.section) ?? new Set()).add(e.scope));
    // The model page alone holds three different owners: one per-provider
    // value, three machine-wide ones, and nothing session-local.
    expect(bySection.get("model")?.size, "the model page's scopes collapsed to one — was scope derived from the section?")
      .toBeGreaterThan(1);
    // And two pages that look alike disagree: preset is not persisted at all,
    // approval is persisted machine-wide, and they render identically.
    expect(SETTING_AT("preset")?.scope).toBe("session");
    expect(SETTING_AT("approval")?.scope).toBe("machine");
  });

  // Keywords buy a way in. They are not the setting's name, and nothing may
  // read them as one: a search alias that shadowed a title would make two
  // settings answer to one word and neither of them be the one meant.
  it("keeps aliases out of the name space", () => {
    const titles = new Set(SETTINGS.map((e) => e.title));
    const clashing = SETTINGS.flatMap((e) => (e.keywords ?? []).filter((k) => titles.has(k)).map((k) => `${e.anchor}: ${k}`));
    expect(clashing, "an alias spelled exactly like some setting's title").toEqual([]);
  });

  // Two blocks alike in every way a reader or a compiler can see — same title,
  // same shape, same absence of children — and they say different things
  // because the table says different things. Nothing here is inferred from the
  // wording, the handler or the component: change the declaration and the row
  // changes, change anything else and it does not.
  it("reads what a change costs from the declaration and nowhere else", () => {
    const now = renderToStaticMarkup(<Group id="preset" title="X" />);
    const later = renderToStaticMarkup(<Group id="storage" title="X" />);
    expect(now).toContain("立即生效");
    expect(later).toContain("已保存 · 重启后生效");
    expect(now).not.toContain("重启");
  });

  // Saved and in force are two facts, and the restart answer is the one where
  // they come apart. A row that only said "after a restart" would leave the
  // reader wondering whether closing this sheet loses what they just typed.
  it("says a restart setting is already kept", () => {
    expect(renderToStaticMarkup(<Group id="storage" title="X" />)).toContain("已保存");
  });

  // Reporting blocks have nothing to apply, and must not claim otherwise.
  it("promises nothing for a block that changes nothing", () => {
    const html = renderToStaticMarkup(<Group id="usage" title="X" />);
    expect(html).not.toContain("生效");
    expect(html).not.toContain("重建");
  });
});
