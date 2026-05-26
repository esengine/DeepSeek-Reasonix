import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { importSessionsCommand } from "../src/cli/commands/import-sessions.js";
import { loadSessionMessages, sessionPath } from "../src/memory/session.js";

describe("import-sessions command", () => {
  let tmpHome: string;
  let tmpSourceDir: string;

  beforeEach(() => {
    tmpHome = mkdtempSync(join(tmpdir(), "reasonix-import-home-"));
    tmpSourceDir = mkdtempSync(join(tmpdir(), "reasonix-import-src-"));
    vi.stubEnv("USERPROFILE", tmpHome);
    vi.stubEnv("HOME", tmpHome);
    vi.spyOn(require("node:os"), "homedir").mockReturnValue(tmpHome);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    if (existsSync(tmpHome)) rmSync(tmpHome, { recursive: true, force: true });
    if (existsSync(tmpSourceDir)) rmSync(tmpSourceDir, { recursive: true, force: true });
  });

  it("imports a Claude session into Reasonix storage and writes meta", () => {
    const sourcePath = join(tmpSourceDir, "claude.jsonl");
    writeFileSync(
      sourcePath,
      [
        JSON.stringify({
          type: "user",
          cwd: "/tmp/project-a",
          message: { role: "user", content: "Audit the branch" },
        }),
        JSON.stringify({
          type: "assistant",
          message: { role: "assistant", content: [{ type: "text", text: "Done." }] },
        }),
      ].join("\n"),
      "utf8",
    );

    importSessionsCommand({
      source: "claude",
      path: sourcePath,
      name: "imported-audit",
      workspace: "/tmp/override-workspace",
      summary: "Imported from Claude",
    });

    expect(loadSessionMessages("imported-audit")).toEqual([
      { role: "user", content: "Audit the branch" },
      { role: "assistant", content: "Done.", tool_calls: undefined, reasoning_content: undefined },
    ]);
    const meta = JSON.parse(
      readFileSync(sessionPath("imported-audit").replace(/\.jsonl$/, ".meta.json"), "utf8"),
    ) as { workspace?: string; summary?: string };
    expect(meta.workspace).toBe("/tmp/override-workspace");
    expect(meta.summary).toBe("Imported from Claude");
  });
});
