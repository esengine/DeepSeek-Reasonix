import { expect, it } from "vitest";

const CSS = import.meta.glob("./settings-type.css", { query: "?raw", import: "default", eager: true }) as Record<string, string>;
const css = CSS["./settings-type.css"]!;
const declarations = [...css.matchAll(/([a-z-]+)\s*:\s*([^;}]+)[;}]/g)].map((m) => ({ prop: m[1]!, value: m[2]!.trim() }));

it("keeps the settings scope on two neutral text tiers and one emphasis weight", () => {
  const scope = css.match(/\.prefs-col\s*\{([^}]*)\}/)?.[1] ?? "";
  expect(scope).toMatch(/--faint:\s*var\(--muted\)/);
  expect(scope).toMatch(/--ghost:\s*var\(--muted\)/);
  expect(scope).toMatch(/--fs-micro:\s*var\(--fs-meta\)/);
  expect(scope).toMatch(/--w-med:\s*var\(--w-semi\)/);
});

it("only ever names a neutral text colour from the two tiers", () => {
  for (const d of declarations.filter((x) => x.prop === "color")) expect(d.value).toMatch(/^var\(--(text|muted)\)$/);
});
