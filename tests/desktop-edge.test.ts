/** Regression net for issue #1180 — `derivePrefix` must not drift between the CLI
 *  re-export path and the direct core-utils import path (used by Desktop). */

import { derivePrefix as fromCoreUtils } from "@reasonix/core-utils";
import { describe, expect, it } from "vitest";
import { derivePrefix as fromCliPath } from "../src/tools/shell/parse.js";

describe("derivePrefix parity — CLI path vs core-utils path", () => {
  const cases = [
    "ls",
    "npm test",
    "npm install lodash",
    "git status",
    "git commit -m hi",
    "cargo add serde",
    "docker run --rm foo",
    "python3 -m pytest tests/",
    "node script.js",
    "./build.sh --release",
    "",
    "   ",
  ];

  it("re-export path and core-utils path return identical results for all cases", () => {
    for (const cmd of cases) {
      expect(fromCliPath(cmd)).toBe(fromCoreUtils(cmd));
    }
  });
});
