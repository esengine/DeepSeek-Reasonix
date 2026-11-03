import { describe, expect, it } from "vitest";
import tokens from "./tokens.css?raw";

const declared = (name: string) => {
  const m = new RegExp(`${name}:\\s*([^;]+);`).exec(tokens);
  return m ? m[1].trim() : "";
};

describe("the design spec's tokens", () => {
  it("steps the corner radius 5 / 7 / 10 / 15 and keeps the pill", () => {
    expect(declared("--r-xs")).toBe("calc(5px * var(--r-scale, 1))");
    expect(declared("--r-sm")).toBe("calc(7px * var(--r-scale, 1))");
    expect(declared("--r-md")).toBe("calc(10px * var(--r-scale, 1))");
    expect(declared("--r-lg")).toBe("calc(15px * var(--r-scale, 1))");
    expect(declared("--r-pill")).toBe("999px");
  });

  it("sets six type sizes 11 / 12 / 13 / 14 / 17 / 22", () => {
    const sizes = ["--fs-micro", "--fs-meta", "--fs-ui", "--fs-lead", "--fs-title", "--fs-display"].map((n) => /calc\(([0-9]+)px/.exec(declared(n))?.[1]);
    expect(sizes).toEqual(["11", "12", "13", "14", "17", "22"]);
  });

  it("times a line, a card and a panel 120 / 180 / 260 ms", () => {
    expect(/--t-line:\s*([0-9.]+s)/.exec(tokens)?.[1]).toBe(".12s");
    expect(/--t-card:\s*([0-9.]+s)/.exec(tokens)?.[1]).toBe(".18s");
    expect(/--t-panel:\s*([0-9.]+s)/.exec(tokens)?.[1]).toBe(".26s");
  });

  it("keeps the instant tiers the spec names: press 60 ms, tip delay 300 ms, live 600 ms", () => {
    expect(/--t-press:\s*([0-9.]+s)/.exec(tokens)?.[1]).toBe(".06s");
    expect(/--tip-delay:\s*([0-9.]+s)/.exec(tokens)?.[1]).toBe(".3s");
    expect(/--t-live:\s*([0-9.]+s)/.exec(tokens)?.[1]).toBe(".6s");
  });
});
