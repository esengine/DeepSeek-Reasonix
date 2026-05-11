/** `reasonix doctor --json` — structured report shape and exit-code semantics. */

import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { type DoctorCheck, doctorCommand, formatDoctorJson } from "../src/cli/commands/doctor.js";
import { VERSION } from "../src/version.js";

describe("formatDoctorJson", () => {
  it("emits version, summary, and {id,status,message} per check", () => {
    const checks: DoctorCheck[] = [
      { id: "api-key", label: "api key", level: "ok", detail: "set via env" },
      { id: "tokenizer", label: "tokenizer", level: "warn", detail: "fallback" },
      { id: "api-reach", label: "api reach", level: "fail", detail: "boom" },
    ];
    const parsed = JSON.parse(formatDoctorJson(checks, "0.18.1"));

    expect(parsed.version).toBe("0.18.1");
    expect(parsed.summary).toEqual({ ok: 1, warn: 1, fail: 1 });
    expect(parsed.checks).toEqual([
      { id: "api-key", status: "ok", message: "set via env" },
      { id: "tokenizer", status: "warn", message: "fallback" },
      { id: "api-reach", status: "fail", message: "boom" },
    ]);
  });

  it("produces a single-line, jq-parseable document", () => {
    const out = formatDoctorJson(
      [{ id: "api-key", label: "api key", level: "ok", detail: "set" }],
      "1.2.3",
    );
    expect(out).not.toContain("\n");
    expect(() => JSON.parse(out)).not.toThrow();
  });

  it("counts an empty check list as all zeros", () => {
    const parsed = JSON.parse(formatDoctorJson([], VERSION));
    expect(parsed.summary).toEqual({ ok: 0, warn: 0, fail: 0 });
    expect(parsed.checks).toEqual([]);
  });
});

describe("doctorCommand --json (integration)", () => {
  let logSpy: ReturnType<typeof vi.spyOn>;
  let exitSpy: ReturnType<typeof vi.spyOn>;
  let tmpHome: string;
  let tmpCwd: string;
  const origCwd = process.cwd();

  beforeEach(() => {
    tmpHome = mkdtempSync(join(tmpdir(), "reasonix-doctor-home-"));
    tmpCwd = mkdtempSync(join(tmpdir(), "reasonix-doctor-cwd-"));
    vi.stubEnv("HOME", tmpHome);
    vi.stubEnv("USERPROFILE", tmpHome);
    // Ensure no API key so checkApiReach skips the network call.
    vi.stubEnv("DEEPSEEK_API_KEY", "");
    process.chdir(tmpCwd);
    logSpy = vi.spyOn(console, "log").mockImplementation(() => {});
    exitSpy = vi.spyOn(process, "exit").mockImplementation(() => undefined as never);
  });

  afterEach(() => {
    logSpy.mockRestore();
    exitSpy.mockRestore();
    vi.unstubAllEnvs();
    process.chdir(origCwd);
    rmSync(tmpHome, { recursive: true, force: true });
    rmSync(tmpCwd, { recursive: true, force: true });
  });

  it("emits exactly one line of valid JSON when --json is set", async () => {
    await doctorCommand({ json: true });

    // No header, no per-check prints, no summary leak — only the JSON document.
    expect(logSpy).toHaveBeenCalledTimes(1);
    const out = String(logSpy.mock.calls[0]![0]);
    const parsed = JSON.parse(out);

    expect(parsed.version).toBe(VERSION);
    expect(parsed.summary).toMatchObject({
      ok: expect.any(Number),
      warn: expect.any(Number),
      fail: expect.any(Number),
    });
    expect(Array.isArray(parsed.checks)).toBe(true);
    for (const c of parsed.checks) {
      expect(typeof c.id).toBe("string");
      expect(["ok", "warn", "fail"]).toContain(c.status);
      expect(typeof c.message).toBe("string");
    }
  });

  it("exits 1 when the report contains any fail status", async () => {
    // checkApiKey returns `fail` when neither env nor config has a key —
    // our temp HOME has no config, and we deleted DEEPSEEK_API_KEY.
    await doctorCommand({ json: true });

    const parsed = JSON.parse(String(logSpy.mock.calls[0]![0]));
    if (parsed.summary.fail > 0) {
      expect(exitSpy).toHaveBeenCalledWith(1);
    } else {
      expect(exitSpy).not.toHaveBeenCalled();
    }
  });
});
