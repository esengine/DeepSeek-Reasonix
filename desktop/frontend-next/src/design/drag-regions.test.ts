import { expect, it } from "vitest";

const SHEETS = import.meta.glob(["../styles/*.css", "./*.css"], { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const css = Object.values(SHEETS).join("\n");

// WindowControls is drawn in these bars. A bar that is a drag region swallows
// the clicks of everything inside it that does not opt out, and on frameless
// Windows the controls are the only way to minimise, zoom or close the window.
const BARS = [".chrome", ".onb-brandbar", ".prefs-windowbar"];

function rulesFor(bar: string): { selector: string; body: string }[] {
  return [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
    .map((m) => ({ selector: m[1]!.trim(), body: m[2]! }))
    .filter((r) => r.selector.split(",").some((s) => s.includes(bar)));
}

for (const bar of BARS) {
  it(`${bar} is a drag region whose buttons opt out of it`, () => {
    const rules = rulesFor(bar);
    expect(rules.some((r) => /-webkit-app-region:\s*drag/.test(r.body)), `${bar} should be a drag region`).toBe(true);
    const optOut = rules.filter((r) => /-webkit-app-region:\s*no-drag/.test(r.body) && /\bbutton\b/.test(r.selector));
    expect(optOut.length, `${bar} buttons (the window controls) must be no-drag`).toBeGreaterThan(0);
  });
}
