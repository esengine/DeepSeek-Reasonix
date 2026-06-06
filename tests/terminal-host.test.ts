import { describe, expect, it } from "vitest";
import { isLegacyWindowsConsole } from "../src/cli/ui/terminal-host.ts";

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
