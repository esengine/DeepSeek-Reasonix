import { describe, expect, it } from "vitest";
import { PRESETS, canonicalPresetName, resolvePreset } from "../src/cli/ui/presets.js";

describe("resolvePreset", () => {
  it("resolves built-in presets by canonical name", () => {
    expect(resolvePreset("flash").model).toBe("deepseek-v4-flash");
    expect(resolvePreset("pro").model).toBe("deepseek-v4-pro");
    expect(resolvePreset("pro").reasoningEffort).toBe("max");
  });

  it("falls back to flash when given undefined", () => {
    expect(resolvePreset(undefined)).toMatchObject(resolvePreset("flash"));
  });

  it("falls back to flash when given legacy or unknown values", () => {
    for (const name of ["auto", "smart", "fast", "max", "definitely-not-a-preset"]) {
      expect(resolvePreset(name)).toMatchObject(resolvePreset("flash"));
    }
  });
});

describe("canonicalPresetName", () => {
  it("returns canonical names for built-ins", () => {
    expect(canonicalPresetName("flash")).toBe("flash");
    expect(canonicalPresetName("pro")).toBe("pro");
  });

  it("normalizes legacy / unknown values to flash", () => {
    for (const name of ["auto", "smart", "fast", "max", "old", undefined]) {
      expect(canonicalPresetName(name)).toBe("flash");
    }
  });
});

describe("preset invariants", () => {
  it("only exposes flash / pro names", () => {
    expect(Object.keys(PRESETS).sort()).toEqual(["flash", "pro"]);
  });
});
