import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { sessionsCommand } from "../src/cli/commands/sessions.js";
import { appendSessionMessage, patchSessionMeta } from "../src/memory/session.js";

describe("sessions command", () => {
  let tmp: string;

  beforeEach(() => {
    tmp = mkdtempSync(join(tmpdir(), "reasonix-sessions-command-"));
    vi.stubEnv("USERPROFILE", tmp);
    vi.stubEnv("HOME", tmp);
    vi.spyOn(require("node:os"), "homedir").mockReturnValue(tmp);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    if (existsSync(tmp)) rmSync(tmp, { recursive: true, force: true });
  });

  it("prints identifying metadata for saved sessions", () => {
    appendSessionMessage("release-fix", { role: "user", content: "repair packaging" });
    patchSessionMeta("release-fix", {
      summary: "Fix release packaging after optional renderer dependency update",
      workspace: "/work/reasonix",
      branch: "GTC/fix-release",
    });
    const lines: string[] = [];
    vi.spyOn(console, "log").mockImplementation((...args: unknown[]) => {
      lines.push(args.map(String).join(" "));
    });

    sessionsCommand({});

    const output = lines.join("\n");
    expect(output).toContain("release-fix");
    expect(output).toContain(
      "summary: Fix release packaging after optional renderer dependency update",
    );
    expect(output).toContain("workspace: reasonix");
    expect(output).toContain("branch: GTC/fix-release");
  });
});
