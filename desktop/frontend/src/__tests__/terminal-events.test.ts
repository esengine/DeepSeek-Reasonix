import { JSDOM } from "jsdom";

import { __emitMockTerminalExit, __emitMockTerminalOutput } from "../lib/bridge";
import {
  __resetTerminalEventBus,
  forgetTerminalSession,
  registerTerminalSink,
  startTerminalEventBridge,
  terminalEventBufferLimit,
} from "../lib/terminalEvents";

function base64(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString("base64");
}

function check(condition: unknown, message: string): void {
  if (!condition) throw new Error(message);
  process.stdout.write(`PASS ${message}\n`);
}

const dom = new JSDOM("<!doctype html><html><body></body></html>");
const previousWindow = globalThis.window;
const previousAtob = globalThis.atob;
const previousBtoa = globalThis.btoa;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.atob = (value: string) => Buffer.from(value, "base64").toString("binary");
globalThis.btoa = (value: string) => Buffer.from(value, "binary").toString("base64");

try {
  __resetTerminalEventBus();
  startTerminalEventBridge();
  const first: number[] = [];
  __emitMockTerminalOutput({ id: "utf8", data: base64(new Uint8Array([0xe4, 0xbd])) });
  __emitMockTerminalOutput({ id: "utf8", data: base64(new Uint8Array([0xa0])) });
  const unregister = registerTerminalSink("utf8", (bytes) => first.push(...bytes));
  unregister();
  check(JSON.stringify(first) === JSON.stringify([0xe4, 0xbd, 0xa0]), "split UTF-8 output is delivered as original bytes");

  const live: number[] = [];
  const unregisterLive = registerTerminalSink("replay", (bytes) => live.push(...bytes));
  __emitMockTerminalOutput({ id: "replay", data: base64(new Uint8Array([1, 2])) });
  unregisterLive();
  __emitMockTerminalOutput({ id: "replay", data: base64(new Uint8Array([3])) });
  const replayed: number[] = [];
  const unregisterReplay = registerTerminalSink("replay", (bytes) => replayed.push(...bytes));
  unregisterReplay();
  check(JSON.stringify(live) === JSON.stringify([1, 2]), "attached terminal receives live output");
  check(JSON.stringify(replayed) === JSON.stringify([1, 2, 3]), "remounted terminal replays complete output history");

  const chunk = new Uint8Array(64 * 1024);
  chunk.fill(65);
  for (let index = 0; index < 24; index += 1) {
    __emitMockTerminalOutput({ id: "bounded", data: base64(chunk) });
  }
  const bounded: number[] = [];
  const unregisterBounded = registerTerminalSink("bounded", (bytes) => bounded.push(...bytes));
  unregisterBounded();
  check(bounded.length <= terminalEventBufferLimit, "terminal output history is bounded to one MiB per session");

  __emitMockTerminalOutput({ id: "forgotten", data: base64(new Uint8Array([9])) });
  forgetTerminalSession("forgotten");
  const forgotten: number[] = [];
  const unregisterForgotten = registerTerminalSink("forgotten", (bytes) => forgotten.push(...bytes));
  unregisterForgotten();
  check(forgotten.length === 0, "explicitly closed terminal history is discarded");

  __emitMockTerminalOutput({ id: "natural-exit", data: base64(new Uint8Array([7])) });
  __emitMockTerminalExit({ id: "natural-exit", exitCode: 0 });
  const naturalExit: number[] = [];
  const unregisterNaturalExit = registerTerminalSink("natural-exit", (bytes) => naturalExit.push(...bytes));
  unregisterNaturalExit();
  check(JSON.stringify(naturalExit) === JSON.stringify([7]), "natural exit preserves terminal history");

  __emitMockTerminalOutput({ id: "removed-exit", data: base64(new Uint8Array([8])) });
  __emitMockTerminalExit({ id: "removed-exit", exitCode: 0, removed: true });
  const removedExit: number[] = [];
  const unregisterRemovedExit = registerTerminalSink("removed-exit", (bytes) => removedExit.push(...bytes));
  unregisterRemovedExit();
  check(removedExit.length === 0, "removed terminal exit discards terminal history");
} finally {
  __resetTerminalEventBus();
  if (previousWindow) globalThis.window = previousWindow;
  else delete (globalThis as { window?: unknown }).window;
  if (previousAtob) globalThis.atob = previousAtob;
  if (previousBtoa) globalThis.btoa = previousBtoa;
}
