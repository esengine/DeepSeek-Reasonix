import { describe, expect, it } from "vitest";
import { derivePrefix } from "../src/derive-prefix.js";

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
    expect(derivePrefix("node script.js")).toBe("node");
    expect(derivePrefix("./build.sh --release")).toBe("./build.sh");
  });

  it("handles empty / whitespace input", () => {
    expect(derivePrefix("")).toBe("");
    expect(derivePrefix("   ")).toBe("");
    expect(derivePrefix("  ls  ")).toBe("ls");
  });
});
