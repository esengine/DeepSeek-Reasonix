import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { suggestSlashCommands } from "../src/cli/ui/slash.js";
import { loadSlashUsage, recordSlashUse, slashUsagePath } from "../src/slash-usage.js";

let dir: string;
let prevEnv: string | undefined;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "reasonix-usage-"));
  prevEnv = process.env.REASONIX_SLASH_USAGE_PATH;
  process.env.REASONIX_SLASH_USAGE_PATH = join(dir, "slash-usage.json");
});

afterEach(() => {
  if (prevEnv === undefined) {
    // biome-ignore lint/performance/noDelete: process.env must lose the key, not hold "undefined"
    delete process.env.REASONIX_SLASH_USAGE_PATH;
  } else {
    process.env.REASONIX_SLASH_USAGE_PATH = prevEnv;
  }
  rmSync(dir, { recursive: true, force: true });
});

describe("slash-usage store", () => {
  it("returns empty when the file doesn't exist yet", () => {
    expect(loadSlashUsage()).toEqual({});
  });

  it("recordSlashUse persists to disk and survives reload", () => {
    recordSlashUse("status");
    recordSlashUse("status");
    recordSlashUse("compact");
    expect(loadSlashUsage()).toEqual({ status: 2, compact: 1 });
    expect(existsSync(slashUsagePath())).toBe(true);
  });

  it("merges concurrent counts via read-modify-write", () => {
    recordSlashUse("status");
    writeFileSync(
      slashUsagePath(),
      JSON.stringify({ version: 1, counts: { status: 5, retry: 3 } }),
      "utf8",
    );
    const after = recordSlashUse("status");
    expect(after.status).toBe(6);
    expect(after.retry).toBe(3);
  });

  it("ignores garbage payloads instead of crashing", () => {
    writeFileSync(slashUsagePath(), "not-json", "utf8");
    expect(loadSlashUsage()).toEqual({});
    writeFileSync(slashUsagePath(), JSON.stringify({ counts: { x: "lol" } }), "utf8");
    expect(loadSlashUsage()).toEqual({});
    writeFileSync(slashUsagePath(), JSON.stringify({ counts: { x: -5 } }), "utf8");
    expect(loadSlashUsage()).toEqual({});
  });

  it("writes a stable on-disk shape", () => {
    recordSlashUse("status");
    const raw = JSON.parse(readFileSync(slashUsagePath(), "utf8"));
    expect(raw).toEqual({ version: 1, counts: { status: 1 } });
  });
});

describe("suggestSlashCommands frequency sort", () => {
  it("preserves declared order when no counts are passed", () => {
    const cmds = suggestSlashCommands("h").map((s) => s.cmd);
    expect(cmds).toEqual(["help", "hooks"]);
  });

  it("sorts higher-count commands first within the filtered set", () => {
    const sorted = suggestSlashCommands("h", false, { hooks: 50, help: 1 }).map((s) => s.cmd);
    expect(sorted[0]).toBe("hooks");
    expect(sorted).toContain("help");
  });

  it("falls back to declared order when counts tie", () => {
    const sorted = suggestSlashCommands("h", false, {}).map((s) => s.cmd);
    expect(sorted).toEqual(["help", "hooks"]);
  });

  it("ignores counts for commands outside the filter set", () => {
    const sorted = suggestSlashCommands("h", false, { status: 9999 }).map((s) => s.cmd);
    expect(sorted).toEqual(["help", "hooks"]);
  });
});
