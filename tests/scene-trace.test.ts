import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import process from "node:process";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { frame, text } from "../src/cli/ui/scene/build.js";
import {
  emitSceneFrame,
  flushSceneTrace,
  isSceneTraceEnabled,
  resetSceneTrace,
} from "../src/cli/ui/scene/trace.js";

const STUB = "tests/fixtures/scene-echo-stub.mjs";

const ENV_KEYS = [
  "REASONIX_SCENE_TRACE",
  "REASONIX_RENDERER",
  "REASONIX_RENDER_CMD",
  "SCENE_ECHO_OUT",
] as const;
type EnvKey = (typeof ENV_KEYS)[number];

describe("scene trace harness", () => {
  let dir: string;
  let filePath: string;
  let stubOut: string;
  let prev: Partial<Record<EnvKey, string | undefined>>;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "scene-trace-"));
    filePath = join(dir, "frames.jsonl");
    stubOut = join(dir, "child-frames.jsonl");
    prev = Object.fromEntries(ENV_KEYS.map((k) => [k, process.env[k]])) as typeof prev;
    for (const k of ENV_KEYS) {
      delete process.env[k];
    }
    resetSceneTrace();
  });

  afterEach(async () => {
    await flushSceneTrace();
    resetSceneTrace();
    for (const k of ENV_KEYS) {
      const v = prev[k];
      if (v === undefined) {
        delete process.env[k];
      } else {
        process.env[k] = v;
      }
    }
    rmSync(dir, { recursive: true, force: true });
  });

  it("is disabled when no env vars are set", () => {
    expect(isSceneTraceEnabled()).toBe(false);
    emitSceneFrame(frame(80, 24, text("noop")));
  });

  it("is disabled when REASONIX_SCENE_TRACE is empty", () => {
    process.env.REASONIX_SCENE_TRACE = "";
    resetSceneTrace();
    expect(isSceneTraceEnabled()).toBe(false);
  });

  it("writes each emitted frame as one JSONL line in file mode", () => {
    process.env.REASONIX_SCENE_TRACE = filePath;
    resetSceneTrace();
    expect(isSceneTraceEnabled()).toBe(true);
    emitSceneFrame(frame(80, 24, text("first")));
    emitSceneFrame(frame(120, 40, text("second")));
    const lines = readFileSync(filePath, "utf8").trimEnd().split("\n");
    expect(lines).toHaveLength(2);
    expect(JSON.parse(lines[0]).root.runs[0].text).toBe("first");
    expect(JSON.parse(lines[1]).root.runs[0].text).toBe("second");
  });

  it("truncates the file on init, so a stale trace from a previous run does not bleed", () => {
    process.env.REASONIX_SCENE_TRACE = filePath;
    resetSceneTrace();
    emitSceneFrame(frame(80, 24, text("first run")));
    resetSceneTrace();
    emitSceneFrame(frame(80, 24, text("second run")));
    const lines = readFileSync(filePath, "utf8").trimEnd().split("\n");
    expect(lines).toHaveLength(1);
    expect(JSON.parse(lines[0]).root.runs[0].text).toBe("second run");
  });

  it("dispatches frames to a child process when REASONIX_RENDERER=rust", async () => {
    process.env.REASONIX_RENDERER = "rust";
    process.env.REASONIX_RENDER_CMD = JSON.stringify([process.execPath, STUB]);
    process.env.SCENE_ECHO_OUT = stubOut;
    resetSceneTrace();
    expect(isSceneTraceEnabled()).toBe(true);
    emitSceneFrame(frame(80, 24, text("via-child")));
    await flushSceneTrace();
    const lines = readFileSync(stubOut, "utf8").trimEnd().split("\n");
    expect(lines).toHaveLength(1);
    expect(JSON.parse(lines[0]).root.runs[0].text).toBe("via-child");
  });

  it("child mode wins over file mode when both env vars are set", async () => {
    process.env.REASONIX_RENDERER = "rust";
    process.env.REASONIX_RENDER_CMD = JSON.stringify([process.execPath, STUB]);
    process.env.SCENE_ECHO_OUT = stubOut;
    process.env.REASONIX_SCENE_TRACE = filePath;
    resetSceneTrace();
    emitSceneFrame(frame(80, 24, text("priority")));
    await flushSceneTrace();
    const childLines = readFileSync(stubOut, "utf8").trimEnd().split("\n");
    expect(childLines).toHaveLength(1);
    expect(() => readFileSync(filePath, "utf8")).toThrow();
  });
});
