import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import process from "node:process";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  createRustKeystrokeReader,
  translate,
  translateMouse,
  translatePaste,
} from "../src/cli/ui/scene/input-adapter.js";
import type {
  KeyInputEvent,
  MouseInputEvent,
  PasteInputEvent,
} from "../src/cli/ui/scene/input-source.js";

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

  function writeEvents(events: (KeyInputEvent | PasteInputEvent | MouseInputEvent)[]): void {
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

  it("forwards paste events as KeyEvent with paste=true", async () => {
    writeEvents([
      { event: "key", code: "Char", char: "a" },
      { event: "paste", text: "hello world" },
      { event: "key", code: "Enter" },
    ]);
    const reader = createRustKeystrokeReader({
      command: [process.execPath, STUB],
      env: { ...process.env, INPUT_EMIT_FILE: eventsPath },
    });
    const received: Array<{ input: string; paste?: boolean; return?: boolean }> = [];
    reader.subscribe((ev) => received.push(ev));
    await reader.wait();
    expect(received).toEqual([
      { input: "a" },
      { input: "hello world", paste: true },
      { input: "", return: true },
    ]);
  });
});

describe("translatePaste", () => {
  it("wraps the text as a KeyEvent with paste=true", () => {
    expect(translatePaste({ event: "paste", text: "hello" })).toEqual({
      input: "hello",
      paste: true,
    });
  });

  it("strips invisible bidi/zero-width chars via sanitizePasteText", () => {
    const polluted = "a\u200Bb\u202Ec";
    const result = translatePaste({ event: "paste", text: polluted });
    expect(result.input).toBe("abc");
    expect(result.paste).toBe(true);
  });
});

describe("translateMouse", () => {
  it("click sets mouseClick=true with row/col", () => {
    expect(translateMouse({ event: "mouse", kind: "click", row: 5, col: 10 })).toEqual({
      input: "",
      mouseClick: true,
      mouseRow: 5,
      mouseCol: 10,
    });
  });

  it("drag sets mouseDrag=true", () => {
    expect(translateMouse({ event: "mouse", kind: "drag", row: 5, col: 11 }).mouseDrag).toBe(true);
  });

  it("release sets mouseRelease=true", () => {
    expect(translateMouse({ event: "mouse", kind: "release", row: 5, col: 11 }).mouseRelease).toBe(
      true,
    );
  });

  it("scroll-up sets mouseScrollUp=true", () => {
    expect(
      translateMouse({ event: "mouse", kind: "scroll-up", row: 5, col: 10 }).mouseScrollUp,
    ).toBe(true);
  });

  it("scroll-down sets mouseScrollDown=true", () => {
    expect(
      translateMouse({ event: "mouse", kind: "scroll-down", row: 5, col: 10 }).mouseScrollDown,
    ).toBe(true);
  });
});

describe("createRustKeystrokeReader forwards mouse events end-to-end", () => {
  let dir: string;
  let eventsPath: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "input-adapter-mouse-"));
    eventsPath = join(dir, "events.jsonl");
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("delivers click + scroll-up + drag in order via the child", async () => {
    writeFileSync(
      eventsPath,
      `${[
        '{"event":"mouse","kind":"click","row":3,"col":7}',
        '{"event":"mouse","kind":"scroll-up","row":3,"col":7}',
        '{"event":"mouse","kind":"drag","row":4,"col":8}',
      ].join("\n")}\n`,
    );
    const reader = createRustKeystrokeReader({
      command: [process.execPath, STUB],
      env: { ...process.env, INPUT_EMIT_FILE: eventsPath },
    });
    const received: import("../src/cli/ui/stdin-reader.js").KeyEvent[] = [];
    reader.subscribe((ev) => received.push(ev));
    await reader.wait();
    expect(received).toHaveLength(3);
    expect(received[0].mouseClick).toBe(true);
    expect(received[0].mouseRow).toBe(3);
    expect(received[0].mouseCol).toBe(7);
    expect(received[1].mouseScrollUp).toBe(true);
    expect(received[2].mouseDrag).toBe(true);
    expect(received[2].mouseCol).toBe(8);
  });
});
