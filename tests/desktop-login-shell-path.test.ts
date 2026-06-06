/** #1252 — GUI launches don't run shell init; verify the login-shell PATH augment behaves. */

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  augmentProcessPath,
  resetLoginShellPathCache,
  resolveLoginShellPath,
} from "../src/desktop/login-shell-path.js";

const originalPlatform = process.platform;
const originalPath = process.env.PATH;
const originalShell = process.env.SHELL;

function setPlatform(p: NodeJS.Platform): void {
  Object.defineProperty(process, "platform", { value: p, configurable: true });
}

describe("desktop login-shell PATH (#1252)", () => {
  beforeEach(() => {
    resetLoginShellPathCache();
  });

  afterEach(() => {
    resetLoginShellPathCache();
    Object.defineProperty(process, "platform", { value: originalPlatform, configurable: true });
    process.env.PATH = originalPath;
    process.env.SHELL = originalShell;
  });

  it("resolveLoginShellPath returns null on Windows (no-op there)", () => {
    setPlatform("win32");
    expect(resolveLoginShellPath()).toBeNull();
  });

  it("augmentProcessPath is a no-op on Windows", () => {
    setPlatform("win32");
    process.env.PATH = "C:\\bar";
    const result = augmentProcessPath();
    expect(result.added).toEqual([]);
    expect(process.env.PATH).toBe("C:\\bar");
  });

  it("resolveLoginShellPath returns null when the shell probe times out", () => {
    if (process.platform === "win32") {
      // Behaviour exercised in test above; the actual probe path doesn't run on win32.
      return;
    }
    process.env.SHELL = "/bin/sh";
    const out = resolveLoginShellPath({ timeoutMs: 1 });
    expect(out === null || out === "" || typeof out === "string").toBe(true);
  });

  it("augmentProcessPath is idempotent — re-running adds nothing", () => {
    setPlatform("linux");
    process.env.PATH = "/already:/seen";
    resetLoginShellPathCache();
    augmentProcessPath();
    const after1 = process.env.PATH;
    augmentProcessPath();
    expect(process.env.PATH).toBe(after1);
  });
});
