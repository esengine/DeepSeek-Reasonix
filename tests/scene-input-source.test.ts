import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import process from "node:process";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { type KeyInputEvent, spawnInputSource } from "../src/cli/ui/scene/input-source.js";

const STUB = "tests/fixtures/input-emit-stub.mjs";

describe("spawnInputSource", () => {
  let dir: string;
  let eventsPath: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "input-source-"));
    eventsPath = join(dir, "events.jsonl");
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  function writeEvents(events: KeyInputEvent[]): void {
    writeFileSync(eventsPath, `${events.map((e) => JSON.stringify(e)).join("\n")}\n`);
  }

  function writeRaw(lines: string[]): void {
    writeFileSync(eventsPath, `${lines.join("\n")}\n`);
  }

  it("forwards key events from the child to registered handlers in order", async () => {
    const events: KeyInputEvent[] = [
      { event: "key", code: "Char", char: "a" },
      { event: "key", code: "Enter" },
      { event: "key", code: "Char", char: "c", modifiers: ["ctrl"] },
    ];
    writeEvents(events);
    const source = spawnInputSource({
      command: [process.execPath, STUB],
      env: { ...process.env, INPUT_EMIT_FILE: eventsPath },
    });
    const received: KeyInputEvent[] = [];
    source.onKey((e) => received.push(e));
    const code = await source.wait();
    expect(code).toBe(0);
    expect(received).toEqual(events);
  });

  it("dispatches to multiple handlers", async () => {
    writeEvents([{ event: "key", code: "Enter" }]);
    const source = spawnInputSource({
      command: [process.execPath, STUB],
      env: { ...process.env, INPUT_EMIT_FILE: eventsPath },
    });
    const a: KeyInputEvent[] = [];
    const b: KeyInputEvent[] = [];
    source.onKey((e) => a.push(e));
    source.onKey((e) => b.push(e));
    await source.wait();
    expect(a).toHaveLength(1);
    expect(b).toHaveLength(1);
  });

  it("stops calling a handler after its unsubscriber runs", async () => {
    writeEvents([
      { event: "key", code: "Char", char: "a" },
      { event: "key", code: "Char", char: "b" },
    ]);
    const source = spawnInputSource({
      command: [process.execPath, STUB],
      env: { ...process.env, INPUT_EMIT_FILE: eventsPath },
    });
    const received: KeyInputEvent[] = [];
    const unsubscribe = source.onKey((e) => {
      received.push(e);
      if (received.length === 1) unsubscribe();
    });
    await source.wait();
    expect(received).toHaveLength(1);
  });

  it("skips malformed lines without crashing or dropping later events", async () => {
    writeRaw(['{"event":"key","code":"Enter"}', "not-json", '{"event":"key","code":"Esc"}', ""]);
    const source = spawnInputSource({
      command: [process.execPath, STUB],
      env: { ...process.env, INPUT_EMIT_FILE: eventsPath },
    });
    const received: KeyInputEvent[] = [];
    source.onKey((e) => received.push(e));
    await source.wait();
    expect(received.map((e) => e.code)).toEqual(["Enter", "Esc"]);
  });

  it("ignores JSON objects that aren't key events", async () => {
    writeRaw(['{"event":"resize","cols":120}', '{"event":"key","code":"Enter"}']);
    const source = spawnInputSource({
      command: [process.execPath, STUB],
      env: { ...process.env, INPUT_EMIT_FILE: eventsPath },
    });
    const received: KeyInputEvent[] = [];
    source.onKey((e) => received.push(e));
    await source.wait();
    expect(received).toHaveLength(1);
    expect(received[0].code).toBe("Enter");
  });

  it("throws synchronously on an empty command", () => {
    expect(() => spawnInputSource({ command: [] })).toThrow(/empty command/);
  });
});
