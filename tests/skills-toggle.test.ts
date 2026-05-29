/** toggleSkillDisabled — config-level enable/disable of individual skills. */

import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { toggleSkillDisabled } from "../src/config.js";

describe("toggleSkillDisabled", () => {
  let tmpDir: string;
  let configPath: string;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), "skill-toggle-"));
    configPath = join(tmpDir, "config.json");
  });
  afterEach(() => rmSync(tmpDir, { recursive: true, force: true }));

  function readCfg(): Record<string, unknown> {
    return JSON.parse(readFileSync(configPath, "utf8"));
  }

  function writeCfg(cfg: Record<string, unknown>): void {
    writeFileSync(configPath, JSON.stringify(cfg, null, 2), "utf8");
  }

  it("disable adds name to config skills.disabled", () => {
    writeCfg({});
    const result = toggleSkillDisabled("disable", "explore", configPath);
    expect(result).toEqual({ ok: true, enabled: false });
    const cfg = readCfg();
    expect(cfg.skills).toEqual({ disabled: ["explore"] });
  });

  it("disable on already-disabled returns ok (idempotent)", () => {
    writeCfg({ skills: { disabled: ["explore"] } });
    const result = toggleSkillDisabled("disable", "explore", configPath);
    expect(result).toEqual({ ok: true, enabled: false });
    const cfg = readCfg();
    expect(cfg.skills).toEqual({ disabled: ["explore"] });
  });

  it("enable removes name from config skills.disabled", () => {
    writeCfg({ skills: { disabled: ["alpha", "beta", "gamma"] } });
    const result = toggleSkillDisabled("enable", "beta", configPath);
    expect(result).toEqual({ ok: true, enabled: true });
    const cfg = readCfg();
    expect(cfg.skills).toEqual({ disabled: ["alpha", "gamma"] });
  });

  it("enable on non-disabled returns ok (idempotent)", () => {
    writeCfg({});
    const result = toggleSkillDisabled("enable", "explore", configPath);
    expect(result).toEqual({ ok: true, enabled: true });
    const cfg = readCfg();
    expect(cfg.skills).toBeUndefined();
  });

  it("empty disabled array is cleaned up (not persisted as [])", () => {
    writeCfg({ skills: { disabled: ["only-one"] } });
    toggleSkillDisabled("enable", "only-one", configPath);
    const cfg = readCfg();
    expect(cfg.skills).toEqual({});
  });

  it("preserves other skills config fields", () => {
    writeCfg({ skills: { paths: ["/some/path"] } });
    toggleSkillDisabled("disable", "test", configPath);
    const cfg = readCfg();
    expect(cfg.skills).toEqual({ paths: ["/some/path"], disabled: ["test"] });
  });

  it("returns error for empty name", () => {
    writeCfg({});
    const result = toggleSkillDisabled("disable", "", configPath);
    expect(result).toEqual({ error: "skill name is required" });
  });

  it("disabled list is sorted", () => {
    writeCfg({});
    toggleSkillDisabled("disable", "gamma", configPath);
    toggleSkillDisabled("disable", "alpha", configPath);
    toggleSkillDisabled("disable", "beta", configPath);
    const cfg = readCfg();
    expect(cfg.skills).toEqual({ disabled: ["alpha", "beta", "gamma"] });
  });
});
