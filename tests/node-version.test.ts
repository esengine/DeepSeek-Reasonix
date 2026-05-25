import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import {
  MIN_NODE_MAJOR,
  isSupportedNodeVersion,
  unsupportedNodeMessage,
} from "../src/cli/node-version.js";

describe("CLI Node runtime guard (#1698)", () => {
  it("rejects Node 18 before the CLI loads heavier startup modules", () => {
    expect(isSupportedNodeVersion("18.17.0")).toBe(false);
    expect(unsupportedNodeMessage("18.17.0")).toContain(`Node ${MIN_NODE_MAJOR}+`);
    expect(unsupportedNodeMessage("18.17.0")).toContain("18.17.0");
  });

  it("accepts Node 22 and later", () => {
    expect(isSupportedNodeVersion("22.16.0")).toBe(true);
    expect(isSupportedNodeVersion("24.14.1")).toBe(true);
  });

  it("runs before the heap-limit re-exec hook", () => {
    const source = readFileSync("src/cli/index.ts", "utf8");
    expect(source.indexOf('import "./node-version-guard.js";')).toBeLessThan(
      source.indexOf('import "./heap-limit-launch.js";'),
    );
  });
});
