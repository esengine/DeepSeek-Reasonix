import { describe, expect, it } from "vitest";
import { isBusyPromptCommand } from "../src/cli/ui/App.js";

describe("busy prompt command policy", () => {
  it("allows workflow management slash commands while a turn is busy", () => {
    expect(isBusyPromptCommand("/workflows")).toBe(false);
    expect(isBusyPromptCommand("/workflows show wf_123")).toBe(false);
    expect(isBusyPromptCommand("/workflows stop wf_123")).toBe(false);
  });

  it("still rejects non-workflow commands while a turn is busy", () => {
    expect(isBusyPromptCommand("/status")).toBe(true);
    expect(isBusyPromptCommand("#note")).toBe(true);
    expect(isBusyPromptCommand("!git status")).toBe(true);
  });
});
