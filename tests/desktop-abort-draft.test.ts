import { describe, expect, it } from "vitest";
import { nextAbortDraftCandidate, restoreAbortedDraft } from "../desktop/src/abort-draft";

describe("desktop abort draft restore", () => {
  it("restores interrupted text when the composer draft is empty", () => {
    expect(restoreAbortedDraft("", "rewrite the whole file")).toBe("rewrite the whole file");
  });

  it("does not overwrite a non-empty composer draft", () => {
    expect(restoreAbortedDraft("new idea", "old interrupted prompt")).toBeNull();
  });

  it("does not restore blank or missing interrupted text", () => {
    expect(restoreAbortedDraft("", "")).toBeNull();
    expect(restoreAbortedDraft("", "   ")).toBeNull();
    expect(restoreAbortedDraft("", null)).toBeNull();
  });

  it("records normal user input as the abort draft candidate", () => {
    expect(
      nextAbortDraftCandidate(null, {
        type: "record",
        source: "user_input",
        text: "  rewrite everything  ",
      }),
    ).toBe("rewrite everything");
  });

  it("records the slash command text for skill runs", () => {
    expect(
      nextAbortDraftCandidate(null, {
        type: "record",
        source: "skill_run",
        text: "/review src/App.tsx",
      }),
    ).toBe("/review src/App.tsx");
  });

  it("does not record /btw side questions as abort draft candidates", () => {
    expect(
      nextAbortDraftCandidate("stale", {
        type: "record",
        source: "btw",
        text: "/btw what is the status?",
      }),
    ).toBeNull();
  });

  it("clears stale abort draft candidates", () => {
    const recorded = nextAbortDraftCandidate(null, {
      type: "record",
      source: "user_input",
      text: "keep me only while running",
    });

    expect(nextAbortDraftCandidate(recorded, { type: "clear" })).toBeNull();
  });
});
