import { describe, expect, it } from "vitest";
import { compareVersions } from "../dashboard/src/lib/version.js";

describe("dashboard compareVersions", () => {
  it("treats installed > cached-latest as up-to-date (issue #510)", () => {
    expect(compareVersions("0.35.0", "0.31.0")).toBeGreaterThan(0);
  });

  it("returns 0 for equal versions", () => {
    expect(compareVersions("0.35.0", "0.35.0")).toBe(0);
  });

  it("returns negative when installed < latest", () => {
    expect(compareVersions("0.34.0", "0.35.0")).toBeLessThan(0);
  });

  it("treats pre-release as lower than the bare version", () => {
    expect(compareVersions("0.35.0-rc.1", "0.35.0")).toBeLessThan(0);
    expect(compareVersions("0.35.0", "0.35.0-rc.1")).toBeGreaterThan(0);
  });

  it("orders mismatched part counts numerically", () => {
    expect(compareVersions("1", "1.0.0")).toBe(0);
    expect(compareVersions("1.2.0", "1.2.1")).toBeLessThan(0);
  });
});
