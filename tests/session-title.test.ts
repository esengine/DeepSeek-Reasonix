import { describe, expect, it } from "vitest";
import {
  buildSessionTitleMessages,
  makeSessionNameFromTitle,
  normalizeGeneratedSessionTitle,
  shouldAutoNameSession,
} from "../src/session-title.js";

describe("session title generation", () => {
  it("normalizes model output into a concise title", () => {
    expect(normalizeGeneratedSessionTitle('```text\nTitle: "Fix parser cache bug"\n```')).toBe(
      "Fix parser cache bug",
    );
  });

  it("turns titles into readable safe session names", () => {
    expect(
      makeSessionNameFromTitle("Fix parser cache bug", {
        exists: () => false,
      }),
    ).toBe("Fix-parser-cache-bug");
    expect(
      makeSessionNameFromTitle("修复 会话 损坏", {
        exists: () => false,
      }),
    ).toBe("修复-会话-损坏");
  });

  it("only auto-names default first-turn sessions that have not been named before", () => {
    expect(shouldAutoNameSession("default-20260517123456", {}, 1)).toBe(true);
    expect(shouldAutoNameSession("default-20260517123456", {}, 2)).toBe(false);
    expect(shouldAutoNameSession("custom-session", {}, 1)).toBe(false);
    expect(shouldAutoNameSession("default-20260517123456", { autoTitleGenerated: true }, 1)).toBe(
      false,
    );
  });

  it("builds a no-tools prompt from the conversation head and tail", () => {
    const messages = buildSessionTitleMessages({
      workspace: "/work/reasonix",
      userText: "Please fix the session corruption bug",
      assistantText: "Implemented safer JSONL rewriting and recovery tests.",
    });
    expect(messages).toHaveLength(2);
    expect(messages[0]!.role).toBe("system");
    expect(messages[1]!.content).toContain("Please fix the session corruption bug");
    expect(messages[1]!.content).toContain("Implemented safer JSONL rewriting");
  });
});
