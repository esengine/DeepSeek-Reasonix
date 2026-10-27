// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { boot, STORAGE } from "./index";
import { say } from "./kernel";

afterEach(() => { localStorage.setItem(STORAGE, "zh"); boot(); });

// The refusals internal/runtime/isolation and the isolated task tool emit; a
// tool card renders say(code), so an unworded one shows no reason at all.
const ISOLATION_REFUSALS = ["isolation.unknown_id", "isolation.unavailable", "isolation.not_git", "isolation.apply_conflict", "isolation.unsupported_arg"];

describe("isolation refusals", () => {
  for (const language of ["zh", "en"]) {
    it(`are worded in ${language}`, () => {
      localStorage.setItem(STORAGE, language);
      boot();
      for (const code of ISOLATION_REFUSALS) expect(say({ code }, ""), code).not.toBe("");
    });
  }
});
