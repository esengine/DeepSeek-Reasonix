/** #1074 — the desktop sidecar must survive an unhandled rejection / uncaught exception rather than exit(1) (which Tauri surfaces as "reasonix exited (code 1)" and forces a full reconnect). */

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { installDesktopCrashGuards } from "../src/cli/commands/desktop.js";

function snapshotListeners() {
  return {
    unhandled: process.listeners("unhandledRejection").slice(),
    uncaught: process.listeners("uncaughtException").slice(),
  };
}

function restoreListeners(snap: ReturnType<typeof snapshotListeners>) {
  for (const l of process.listeners("unhandledRejection")) {
    if (!snap.unhandled.includes(l)) process.off("unhandledRejection", l);
  }
  for (const l of process.listeners("uncaughtException")) {
    if (!snap.uncaught.includes(l)) process.off("uncaughtException", l);
  }
}

describe("installDesktopCrashGuards", () => {
  let snap: ReturnType<typeof snapshotListeners>;

  beforeEach(() => {
    snap = snapshotListeners();
  });
  afterEach(() => {
    restoreListeners(snap);
  });

  it("registers one unhandledRejection + one uncaughtException listener", () => {
    installDesktopCrashGuards();
    expect(process.listeners("unhandledRejection").length).toBe(snap.unhandled.length + 1);
    expect(process.listeners("uncaughtException").length).toBe(snap.uncaught.length + 1);
  });

  it("forwards rejection reasons to the injected stderr sink", () => {
    const writes: string[] = [];
    const fakeStderr = { write: (s: string) => writes.push(s) };
    installDesktopCrashGuards(fakeStderr);

    // Find the freshly-installed listener and call it directly — actually
    // emitting the event would race with vitest's own watchers.
    const installed = process.listeners("unhandledRejection").at(-1) as (reason: unknown) => void;
    installed(new Error("synthetic boom"));

    expect(writes).toHaveLength(1);
    expect(writes[0]).toContain("[desktop] unhandledRejection");
    expect(writes[0]).toContain("synthetic boom");
  });

  it("coerces non-Error rejection reasons", () => {
    const writes: string[] = [];
    installDesktopCrashGuards({ write: (s: string) => writes.push(s) });
    const installed = process.listeners("unhandledRejection").at(-1) as (reason: unknown) => void;
    installed("plain string");
    expect(writes[0]).toContain("plain string");
  });

  it("forwards uncaughtException to the same sink", () => {
    const writes: string[] = [];
    installDesktopCrashGuards({ write: (s: string) => writes.push(s) });
    const installed = process.listeners("uncaughtException").at(-1) as (err: Error) => void;
    installed(new Error("synthetic crash"));
    expect(writes).toHaveLength(1);
    expect(writes[0]).toContain("[desktop] uncaughtException");
    expect(writes[0]).toContain("synthetic crash");
  });
});
