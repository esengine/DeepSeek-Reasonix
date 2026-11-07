import { describe, expect, it } from "vitest";
import composer from "./composer.css?raw";

const rule = (selector: string): string => {
  const at = composer.lastIndexOf("\n" + selector + " {");
  return at < 0 ? "" : composer.slice(at, composer.indexOf("}", at));
};

describe("the composer's model pill in a narrow conversation", () => {
  it("lets the model name give way, so the divider and level never spill out of the pill onto the send button", () => {
    const nm = rule(".compose .model-picker .nm");
    expect(nm).toMatch(/flex:\s*0 1 auto/);
    expect(nm).toMatch(/min-width:\s*0/);
    expect(nm).toMatch(/overflow:\s*hidden/);
    expect(nm).toMatch(/text-overflow:\s*ellipsis/);
  });

  it("keeps the pill as wide as the capped control allows, never wider than it", () => {
    expect(rule(".compose .studio-model-control")).toMatch(/min-width:\s*0/);
  });

  it("keeps everything but the name at its own size, and gives a narrow conversation room for the whole pill", () => {
    expect(rule(".compose .model-picker > :not(.nm)")).toMatch(/flex:\s*none/);
    expect(composer).toMatch(/@container conversation \(max-width: 720px\)\s*\{\s*\.compose \.studio-model-control \{ max-width: clamp\(84px, 40cqw, 220px\)/);
  });
});
