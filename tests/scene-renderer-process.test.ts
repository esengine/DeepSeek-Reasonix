import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import process from "node:process";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { frame, text } from "../src/cli/ui/scene/build.js";
import { spawnRenderer } from "../src/cli/ui/scene/renderer-process.js";

const STUB = "tests/fixtures/scene-echo-stub.mjs";

describe("spawnRenderer", () => {
  let dir: string;
  let outPath: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "scene-spawn-"));
    outPath = join(dir, "received.jsonl");
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("forwards each emitted frame to the child as one JSONL line", async () => {
    const proc = spawnRenderer({
      command: [process.execPath, STUB],
      env: { ...process.env, SCENE_ECHO_OUT: outPath },
    });
    proc.emit(frame(80, 24, text("alpha")));
    proc.emit(frame(120, 40, text("beta")));
    const code = await proc.close();
    expect(code).toBe(0);
    const lines = readFileSync(outPath, "utf8").trimEnd().split("\n");
    expect(lines).toHaveLength(2);
    expect(JSON.parse(lines[0]).root.runs[0].text).toBe("alpha");
    expect(JSON.parse(lines[1]).root.runs[0].text).toBe("beta");
  });

  it("close() resolves with the child exit code", async () => {
    const proc = spawnRenderer({
      command: [process.execPath, STUB],
      env: { ...process.env, SCENE_ECHO_OUT: outPath },
    });
    const code = await proc.close();
    expect(code).toBe(0);
  });

  it("emit() after close is a no-op, not an error", async () => {
    const proc = spawnRenderer({
      command: [process.execPath, STUB],
      env: { ...process.env, SCENE_ECHO_OUT: outPath },
    });
    proc.emit(frame(80, 24, text("first")));
    await proc.close();
    expect(() => proc.emit(frame(80, 24, text("after-close")))).not.toThrow();
    const lines = readFileSync(outPath, "utf8").trimEnd().split("\n");
    expect(lines).toHaveLength(1);
  });

  it("throws synchronously on an empty command", () => {
    expect(() => spawnRenderer({ command: [] })).toThrow(/empty command/);
  });
});
