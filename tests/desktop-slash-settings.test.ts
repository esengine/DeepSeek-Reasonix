import { describe, expect, it } from "vitest";
import {
  buildSlashSettingsDescriptors,
  parseSlashSettingsCommand,
} from "../desktop/src/slash-settings";

describe("desktop slash settings", () => {
  it("parses reasoning effort commands", () => {
    expect(parseSlashSettingsCommand("/effort low")).toEqual({
      type: "reasoningEffort",
      reasoningEffort: "low",
    });
  });

  it("parses edit mode commands from model and plan aliases", () => {
    expect(parseSlashSettingsCommand("/model auto")).toEqual({
      type: "editMode",
      editMode: "auto",
    });
    expect(parseSlashSettingsCommand("/plan auto")).toEqual({
      type: "editMode",
      editMode: "auto",
    });
  });

  it("treats bare /plan as plan mode", () => {
    expect(parseSlashSettingsCommand("/plan")).toEqual({
      type: "editMode",
      editMode: "plan",
    });
  });

  it("ignores unknown or incomplete setting commands", () => {
    expect(parseSlashSettingsCommand("/effort turbo")).toBeNull();
    expect(parseSlashSettingsCommand("/model")).toBeNull();
    expect(parseSlashSettingsCommand("/unknown auto")).toBeNull();
  });

  it("exposes setting commands to slash suggestions and help", () => {
    const commands = buildSlashSettingsDescriptors().map((entry) => entry.cmd);

    expect(commands).toContain("/model auto");
    expect(commands).toContain("/plan auto");
    expect(commands).toContain("/effort low");
  });
});
