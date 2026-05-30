import { describe, expect, it } from "vitest";
import {
  canCompleteWorkflowPickerWhileBusy,
  isBusyPromptCommand,
  parseBusyWorkflowSlashCommand,
} from "../src/cli/ui/App.js";

describe("busy prompt command policy", () => {
  it("allows workflow management slash commands while a turn is busy", () => {
    expect(isBusyPromptCommand("/workflows")).toBe(false);
    expect(isBusyPromptCommand("/workflows show wf_123")).toBe(false);
    expect(isBusyPromptCommand("/workflows stop wf_123")).toBe(false);
  });

  it("recognizes workflow slash commands for immediate busy dispatch", () => {
    expect(parseBusyWorkflowSlashCommand("/workflows list")).toEqual({
      cmd: "workflows",
      args: ["list"],
    });
    expect(parseBusyWorkflowSlashCommand("  /workflows show wf_123")).toEqual({
      cmd: "workflows",
      args: ["show", "wf_123"],
    });
    expect(parseBusyWorkflowSlashCommand("/status")).toBeNull();
  });

  it("allows tab completion only for workflow pickers while busy", () => {
    expect(canCompleteWorkflowPickerWhileBusy({ slashCommand: "workflows" })).toBe(true);
    expect(canCompleteWorkflowPickerWhileBusy({ slashArgCommand: "workflows" })).toBe(true);
    expect(canCompleteWorkflowPickerWhileBusy({ slashCommand: "status" })).toBe(false);
    expect(canCompleteWorkflowPickerWhileBusy({ slashArgCommand: "model" })).toBe(false);
  });

  it("still rejects non-workflow commands while a turn is busy", () => {
    expect(isBusyPromptCommand("/status")).toBe(true);
    expect(isBusyPromptCommand("#note")).toBe(true);
    expect(isBusyPromptCommand("!git status")).toBe(true);
  });
});
