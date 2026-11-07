import { describe, expect, it } from "vitest";
import settings from "./settings.css?raw";

const rule = (selector: string): string => {
  const at = settings.indexOf("\n" + selector + " {");
  return at < 0 ? "" : settings.slice(at, settings.indexOf("}", at));
};

describe("the session-message form inside a settings card", () => {
  it("keeps its fields the full width of the form, not the 140px a card gives a select", () => {
    expect(rule(".prefs-card select")).toMatch(/width:\s*140px/);
    const field = rule(".prefs-card select.peerfield");
    expect(field).toMatch(/width:\s*100%/);
    expect(field).toMatch(/text-overflow:\s*ellipsis/);
  });
});
