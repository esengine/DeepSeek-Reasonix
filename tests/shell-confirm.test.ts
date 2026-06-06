import { derivePrefix } from "@reasonix/core-utils";
import { describe, expect, it } from "vitest";

describe("derivePrefix", () => {
  it("returns the sole token for single-word commands", () => {
    expect(derivePrefix("ls")).toBe("ls");
    expect(derivePrefix("pytest")).toBe("pytest");
  });

  it("uses the first two tokens for well-known wrappers", () => {
    expect(derivePrefix("npm install lodash")).toBe("npm install");
    expect(derivePrefix("git commit -m hi")).toBe("git commit");
    expect(derivePrefix("cargo add serde")).toBe("cargo add");
    expect(derivePrefix("docker run --rm foo")).toBe("docker run");
    expect(derivePrefix("python3 -m pytest tests/")).toBe("python3 -m");
  });

  it("falls back to the first token for non-wrapper commands", () => {
    // `node script.js` — the script name is specific to this invocation,
    // so "node" alone is the useful prefix to persist.
    expect(derivePrefix("node script.js")).toBe("node");
    expect(derivePrefix("curl https://api.example.com")).toBe("curl");
  });

  it("normalizes whitespace and returns empty on empty input", () => {
    expect(derivePrefix("   npm   install  ")).toBe("npm install");
    expect(derivePrefix("")).toBe("");
    expect(derivePrefix("   ")).toBe("");
  });
});
