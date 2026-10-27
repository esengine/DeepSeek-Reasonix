// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { boot, STORAGE } from "./index";
import { say } from "./kernel";
import { toolErrorDetail, toolRefusalReason } from "../ui/cards/outcome";
import type { Tool } from "../port/wire";

afterEach(() => { localStorage.setItem(STORAGE, "zh"); boot(); });

// The calls the host stops before they run. The model keeps the English text;
// the person reading the card gets the reason in their own language.
const BLOCKED = [
  "shell.mixed_mutation_verification",
  "shell.inline_interpreter_not_last",
  "batch.skipped_after_failed_change",
  "batch.check_after_stopped_call",
];

describe("calls the host stops before they run", () => {
  for (const language of ["zh", "en"]) {
    it(`are worded in ${language}, and the card shows that instead of the English line`, () => {
      localStorage.setItem(STORAGE, language);
      boot();
      for (const code of BLOCKED) {
        const tool = { name: "bash", args: {}, err: "blocked: raw English wording", refusalCode: code } as unknown as Tool;
        expect(say({ code }, ""), code).not.toBe("");
        expect(toolRefusalReason(tool), code).not.toBe("");
        expect(toolErrorDetail(tool), `${code}: the English line is not drawn when the reason is`).toBe("");
      }
    });
  }
});
