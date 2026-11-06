import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("./studio.css", import.meta.url), "utf8");

describe("the type stacks, as the Type board declares them", () => {
  it("sets code in IBM Plex Mono, with the shipped face loaded", () => {
    expect(css).toMatch(/--mono:\s*"IBM Plex Mono var"/);
    expect(readFileSync(new URL("./tokens.css", import.meta.url), "utf8")).toContain('font-family: "IBM Plex Mono var"');
  });

  it("leads the interface stack with Inter", () => {
    expect(css).toMatch(/--ui:\s*"Inter Studio"/);
  });
});
