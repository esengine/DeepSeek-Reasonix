import { describe, expect, it } from "vitest";
import {
  isLegacyWindowsConsole,
  prefersReducedTerminalPaint,
  terminalFlushIntervalMs,
} from "../src/cli/ui/terminal-host.ts";

const onWindows = process.platform === "win32";

describe("isLegacyWindowsConsole", () => {
  it("returns false on non-Windows hosts regardless of env", () => {
    if (onWindows) return;
    expect(isLegacyWindowsConsole({ WT_SESSION: undefined, TERM_PROGRAM: undefined })).toBe(false);
    expect(isLegacyWindowsConsole({})).toBe(false);
  });

  it("returns false on Windows when WT_SESSION marks Windows Terminal", () => {
    if (!onWindows) return;
    expect(isLegacyWindowsConsole({ WT_SESSION: "{guid}" })).toBe(false);
  });

  it("returns false on Windows when TERM_PROGRAM is set (vscode, etc.)", () => {
    if (!onWindows) return;
    expect(isLegacyWindowsConsole({ TERM_PROGRAM: "vscode" })).toBe(false);
  });

  it("returns true on Windows with neither marker — legacy conhost", () => {
    if (!onWindows) return;
    expect(isLegacyWindowsConsole({})).toBe(true);
  });
});

describe("prefersReducedTerminalPaint", () => {
  it("slows Windows Terminal redraws during streaming turns", () => {
    const env = { WT_SESSION: "{guid}" };

    expect(isLegacyWindowsConsole(env, "win32")).toBe(false);
    expect(prefersReducedTerminalPaint(env, "win32")).toBe(true);
    expect(terminalFlushIntervalMs(env, "win32")).toBe(150);
  });

  it("slows WSL redraws because Windows Terminal still owns the visible paint", () => {
    const env = { WSL_DISTRO_NAME: "Ubuntu-22.04", WT_SESSION: "{guid}" };

    expect(prefersReducedTerminalPaint(env, "linux")).toBe(true);
    expect(terminalFlushIntervalMs(env, "linux")).toBe(150);
  });

  it("keeps ordinary Unix terminals on the responsive flush cadence", () => {
    const env = { TERM_PROGRAM: "iTerm.app" };

    expect(prefersReducedTerminalPaint(env, "darwin")).toBe(false);
    expect(terminalFlushIntervalMs(env, "darwin")).toBe(50);
  });

  it("honors a valid explicit flush override", () => {
    const env = { WT_SESSION: "{guid}", REASONIX_FLUSH_MS: "90" };

    expect(terminalFlushIntervalMs(env, "win32")).toBe(90);
  });
});
