import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import process from "node:process";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { createRustKeystrokeReader, translate } from "../src/cli/ui/scene/input-adapter.js";
import type { KeyInputEvent } from "../src/cli/ui/scene/input-source.js";

const STUB = "tests/fixtures/input-emit-stub.mjs";

describe("translate (KeyInputEvent → KeyEvent)", () => {
  it("plain char becomes one-character input", () => {
    expect(translate({ event: "key", code: "Char", char: "a" })).toEqual({ input: "a" });
  });

  it("Ctrl+letter sets ctrl=true and keeps the char as input", () => {
    expect(translate({ event: "key", code: "Char", char: "c", modifiers: ["ctrl"] })).toEqual({
      input: "c",
      ctrl: true,
    });
  });

  it("Alt+letter maps to meta (Reasonix's name for Alt)", () => {
    expect(translate({ event: "key", code: "Char", char: "k", modifiers: ["alt"] })).toEqual({
      input: "k",
      meta: true,
    });
  });

  it("Shift+letter passes char as-is plus shift=true", () => {
    expect(translate({ event: "key", code: "Char", char: "A", modifiers: ["shift"] })).toEqual({
      input: "A",
      shift: true,
    });
  });

  it("Enter becomes return=true", () => {
    expect(translate({ event: "key", code: "Enter" })).toEqual({ input: "", return: true });
  });

  it("Shift+Enter carries the shift modifier", () => {
    expect(translate({ event: "key", code: "Enter", modifiers: ["shift"] })).toEqual({
      input: "",
      return: true,
      shift: true,
    });
  });

  it.each<
    [string, Partial<KeyInputEvent["code"]>, keyof import("../src/cli/ui/stdin-reader.js").KeyEvent]
  >([
    ["Esc", "Esc", "escape"],
    ["Up", "Up", "upArrow"],
    ["Down", "Down", "downArrow"],
    ["Left", "Left", "leftArrow"],
    ["Right", "Right", "rightArrow"],
    ["Backspace", "Backspace", "backspace"],
    ["Tab", "Tab", "tab"],
    ["Home", "Home", "home"],
    ["End", "End", "end"],
    ["PageUp", "PageUp", "pageUp"],
    ["PageDown", "PageDown", "pageDown"],
    ["Delete", "Delete", "delete"],
  ])("%s maps to the corresponding KeyEvent field", (_label, code, field) => {
    expect(translate({ event: "key", code: code as string })).toEqual({ input: "", [field]: true });
  });

  it("BackTab becomes tab+shift", () => {
    expect(translate({ event: "key", code: "BackTab" })).toEqual({
      input: "",
      tab: true,
      shift: true,
    });
  });

  it("F-keys and unrecognized codes drop to null", () => {
    expect(translate({ event: "key", code: "F5" })).toBeNull();
    expect(translate({ event: "key", code: "CapsLock" })).toBeNull();
  });

  it("Char with no char field drops to null", () => {
    expect(translate({ event: "key", code: "Char" })).toBeNull();
  });
});

describe("createRustKeystrokeReader", () => {
  let dir: string;
  let eventsPath: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "input-adapter-"));
    eventsPath = join(dir, "events.jsonl");
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  function writeEvents(events: KeyInputEvent[]): void {
    writeFileSync(eventsPath, `${events.map((e) => JSON.stringify(e)).join("\n")}\n`);
  }

  it("forwards translated events to subscribers", async () => {
    writeEvents([
      { event: "key", code: "Char", char: "a" },
      { event: "key", code: "Enter" },
      { event: "key", code: "Char", char: "c", modifiers: ["ctrl"] },
    ]);
    const reader = createRustKeystrokeReader({
      command: [process.execPath, STUB],
      env: { ...process.env, INPUT_EMIT_FILE: eventsPath },
    });
    const received: Array<{ input: string; ctrl?: boolean; return?: boolean }> = [];
    reader.start();
    reader.subscribe((ev) => received.push(ev));
    await reader.wait();
    expect(received).toEqual([
      { input: "a" },
      { input: "", return: true },
      { input: "c", ctrl: true },
    ]);
  });

  it("skips events that translate to null", async () => {
    writeEvents([
      { event: "key", code: "F5" },
      { event: "key", code: "Enter" },
    ]);
    const reader = createRustKeystrokeReader({
      command: [process.execPath, STUB],
      env: { ...process.env, INPUT_EMIT_FILE: eventsPath },
    });
    const received: unknown[] = [];
    reader.subscribe((ev) => received.push(ev));
    await reader.wait();
    expect(received).toEqual([{ input: "", return: true }]);
  });

  it("unsubscribe stops further deliveries", async () => {
    writeEvents([
      { event: "key", code: "Char", char: "a" },
      { event: "key", code: "Char", char: "b" },
    ]);
    const reader = createRustKeystrokeReader({
      command: [process.execPath, STUB],
      env: { ...process.env, INPUT_EMIT_FILE: eventsPath },
    });
    const received: unknown[] = [];
    const unsubscribe = reader.subscribe((ev) => {
      received.push(ev);
      if (received.length === 1) unsubscribe();
    });
    await reader.wait();
    expect(received).toHaveLength(1);
  });
});
