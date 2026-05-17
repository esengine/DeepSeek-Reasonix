import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { pushRecentWorkspace } from "../src/config.js";
import { appendSessionMessage, patchSessionMeta } from "../src/memory/session.js";
import { listKnownWorkspaces } from "../src/workspaces.js";

describe("known workspaces", () => {
  let home: string;
  let root: string;
  let current: string;
  let recent: string;
  let fromSession: string;

  beforeEach(() => {
    home = mkdtempSync(join(tmpdir(), "reasonix-workspaces-home-"));
    root = mkdtempSync(join(tmpdir(), "reasonix-workspaces-root-"));
    current = join(root, "current");
    recent = join(root, "recent");
    fromSession = join(root, "from-session");
    mkdirSync(current, { recursive: true });
    mkdirSync(recent, { recursive: true });
    mkdirSync(fromSession, { recursive: true });
    vi.stubEnv("USERPROFILE", home);
    vi.stubEnv("HOME", home);
    vi.spyOn(require("node:os"), "homedir").mockReturnValue(home);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    rmSync(home, { recursive: true, force: true });
    rmSync(root, { recursive: true, force: true });
  });

  it("combines current, recent, and session workspaces without duplicates", () => {
    pushRecentWorkspace(recent);
    pushRecentWorkspace(current);
    appendSessionMessage("session-a", { role: "user", content: "hi" });
    patchSessionMeta("session-a", { workspace: fromSession, branch: "feature/a" });
    appendSessionMessage("session-b", { role: "assistant", content: "done" });
    patchSessionMeta("session-b", { workspace: fromSession, branch: "feature/b" });

    const workspaces = listKnownWorkspaces(current);

    expect(workspaces.map((w) => w.path)).toEqual([current, recent, fromSession]);
    expect(workspaces.find((w) => w.path === current)?.current).toBe(true);
    expect(workspaces.find((w) => w.path === fromSession)?.sessions).toBe(2);
  });
});
