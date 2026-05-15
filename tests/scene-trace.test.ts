import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { frame, text } from "../src/cli/ui/scene/build.js";
import { emitSceneFrame, isSceneTraceEnabled, resetSceneTrace } from "../src/cli/ui/scene/trace.js";

describe("scene trace harness", () => {
  let dir: string;
  let path: string;
  let prev: string | undefined;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "scene-trace-"));
    path = join(dir, "frames.jsonl");
    prev = process.env.REASONIX_SCENE_TRACE;
    resetSceneTrace();
  });

  afterEach(() => {
    if (prev === undefined) {
      // biome-ignore lint/performance/noDelete: env vars are strings; assignment to undefined stores the literal "undefined"
      delete process.env.REASONIX_SCENE_TRACE;
    } else {
      process.env.REASONIX_SCENE_TRACE = prev;
    }
    resetSceneTrace();
    rmSync(dir, { recursive: true, force: true });
  });

  it("is disabled when the env var is unset", () => {
    // biome-ignore lint/performance/noDelete: env vars are strings; assignment to undefined stores the literal "undefined"
    delete process.env.REASONIX_SCENE_TRACE;
    resetSceneTrace();
    expect(isSceneTraceEnabled()).toBe(false);
    emitSceneFrame(frame(80, 24, text("noop")));
  });

  it("is disabled when the env var is empty", () => {
    process.env.REASONIX_SCENE_TRACE = "";
    resetSceneTrace();
    expect(isSceneTraceEnabled()).toBe(false);
  });

  it("writes each emitted frame as one JSONL line", () => {
    process.env.REASONIX_SCENE_TRACE = path;
    resetSceneTrace();
    expect(isSceneTraceEnabled()).toBe(true);
    emitSceneFrame(frame(80, 24, text("first")));
    emitSceneFrame(frame(120, 40, text("second")));
    const lines = readFileSync(path, "utf8").trimEnd().split("\n");
    expect(lines).toHaveLength(2);
    expect(JSON.parse(lines[0])).toEqual({
      schemaVersion: 1,
      cols: 80,
      rows: 24,
      root: { kind: "text", runs: [{ text: "first" }] },
    });
    expect(JSON.parse(lines[1])).toEqual({
      schemaVersion: 1,
      cols: 120,
      rows: 40,
      root: { kind: "text", runs: [{ text: "second" }] },
    });
  });

  it("truncates the file on init, so a stale trace from a previous run does not bleed into the new one", () => {
    process.env.REASONIX_SCENE_TRACE = path;
    resetSceneTrace();
    emitSceneFrame(frame(80, 24, text("first run")));
    resetSceneTrace();
    emitSceneFrame(frame(80, 24, text("second run")));
    const lines = readFileSync(path, "utf8").trimEnd().split("\n");
    expect(lines).toHaveLength(1);
    expect(JSON.parse(lines[0]).root.runs[0].text).toBe("second run");
  });
});
