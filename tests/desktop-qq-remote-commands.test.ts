import { describe, expect, it } from "vitest";
import {
  parseQQRemoteDesktopCommand,
  qqRemoteCommandBypassesBusy,
  qqRemoteDesktopHelpText,
} from "../src/desktop/qq-remote-commands.js";

describe("desktop QQ remote commands", () => {
  const skills = ["agent-reach", "qq"];

  it("parses the desktop-aligned QQ command subset", () => {
    expect(parseQQRemoteDesktopCommand("/help", skills)).toEqual({ kind: "help" });
    expect(parseQQRemoteDesktopCommand("/new", skills)).toEqual({ kind: "new" });
    expect(parseQQRemoteDesktopCommand("/abort", skills)).toEqual({ kind: "abort" });
    expect(parseQQRemoteDesktopCommand("/compact", skills)).toEqual({ kind: "compact" });
    expect(parseQQRemoteDesktopCommand("/retry", skills)).toEqual({ kind: "retry" });
    expect(parseQQRemoteDesktopCommand("/model flash", skills)).toEqual({
      kind: "model",
      value: "flash",
    });
    expect(parseQQRemoteDesktopCommand("/effort high", skills)).toEqual({
      kind: "effort",
      value: "high",
    });
    expect(parseQQRemoteDesktopCommand("/plan auto", skills)).toEqual({
      kind: "plan",
      value: "auto",
    });
    expect(parseQQRemoteDesktopCommand("/btw what's up", skills)).toEqual({
      kind: "btw",
      text: "what's up",
    });
    expect(parseQQRemoteDesktopCommand("/agent-reach latest openai news", skills)).toEqual({
      kind: "skill",
      name: "agent-reach",
      args: "latest openai news",
    });
  });

  it("does not treat UI-only or ambiguous slash text as QQ desktop commands", () => {
    expect(parseQQRemoteDesktopCommand("/theme", skills)).toBeNull();
    expect(parseQQRemoteDesktopCommand("/skill qq", skills)).toBeNull();
    expect(parseQQRemoteDesktopCommand("/btw", skills)).toBeNull();
    expect(parseQQRemoteDesktopCommand("/effort impossible", skills)).toBeNull();
    expect(parseQQRemoteDesktopCommand("/plan maybe", skills)).toBeNull();
    expect(parseQQRemoteDesktopCommand("/unknown", skills)).toBeNull();
  });

  it("allows only help/new/abort/effort to bypass busy", () => {
    expect(qqRemoteCommandBypassesBusy({ kind: "help" })).toBe(true);
    expect(qqRemoteCommandBypassesBusy({ kind: "new" })).toBe(true);
    expect(qqRemoteCommandBypassesBusy({ kind: "abort" })).toBe(true);
    expect(qqRemoteCommandBypassesBusy({ kind: "model", value: "flash" })).toBe(false);
    expect(qqRemoteCommandBypassesBusy({ kind: "effort", value: "high" })).toBe(true);
    expect(qqRemoteCommandBypassesBusy({ kind: "plan", value: "auto" })).toBe(false);
    expect(qqRemoteCommandBypassesBusy({ kind: "compact" })).toBe(false);
    expect(qqRemoteCommandBypassesBusy({ kind: "retry" })).toBe(false);
    expect(qqRemoteCommandBypassesBusy({ kind: "btw", text: "hi" })).toBe(false);
    expect(qqRemoteCommandBypassesBusy({ kind: "skill", name: "qq" })).toBe(false);
  });

  it("mentions the supported remote subset in /help text", () => {
    const help = qqRemoteDesktopHelpText(skills);
    expect(help).toContain("/help");
    expect(help).toContain("/new");
    expect(help).toContain("/abort");
    expect(help).toContain("/compact");
    expect(help).toContain("/retry");
    expect(help).toContain("/model <flash|pro|deepseek-v4-flash|deepseek-v4-pro>");
    expect(help).toContain("/effort <low|medium|high|max>");
    expect(help).toContain("/plan <review|auto|yolo>");
    expect(help).toContain("/btw <question>");
    expect(help).toContain("/<skill> [args]");
    expect(help).toContain("agent-reach, qq");
  });
});
