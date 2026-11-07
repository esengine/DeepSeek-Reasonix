import { describe, expect, it } from "vitest";
import { SETTINGS } from "./prefsnav";
import { searchSettings } from "./settingSearch";
import { INTENTS } from "./settings-intents";

const visible = SETTINGS.filter((e) => !e.hidden);

describe("finding a setting by what a person types", () => {
  it("puts the setting they meant among the first three results", () => {
    const missed = INTENTS.flatMap(([query, want]) => {
      const top = searchSettings(visible, query).slice(0, 3).map((e) => e.anchor);
      return want.some((anchor) => top.includes(anchor)) ? [] : [`${query} -> [${top.join(", ")}], wanted ${want.join(" | ")}`];
    });
    expect(missed).toEqual([]);
  });

  it("only asks for settings the catalogue has", () => {
    const known = new Set(SETTINGS.map((e) => e.anchor));
    const unknown = INTENTS.flatMap(([query, want]) => want.filter((anchor) => !known.has(anchor)).map((anchor) => `${query}: ${anchor}`));
    expect(unknown).toEqual([]);
  });

  it("finds nothing for words that are not about any setting", () => {
    expect(searchSettings(visible, "qzxwv")).toEqual([]);
    expect(searchSettings(visible, "   ")).toEqual([]);
  });

  it("needs every word of a query, so a second word narrows", () => {
    const one = searchSettings(visible, "代理").length;
    const two = searchSettings(visible, "代理 超时").length;
    expect(two).toBeLessThanOrEqual(one);
    expect(two).toBeGreaterThan(0);
  });
});
