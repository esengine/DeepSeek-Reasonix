import assert from "node:assert/strict";
import { JSDOM } from "jsdom";

import {
  clampTerminalSelectionPointToHost,
  normalizeTerminalSelectionText,
  terminalSelectionPointFromHost,
} from "../lib/terminalSelection";

const { document } = new JSDOM("<!doctype html><html><body></body></html>").window;

function mockRect(
  target: Element,
  rect: { left: number; top: number; right: number; bottom: number; width: number; height: number },
): void {
  Object.defineProperty(target, "getBoundingClientRect", { configurable: true, value: () => rect });
}

{
  const host = document.createElement("div");
  mockRect(host, { left: 0, top: 0, right: 800, bottom: 300, width: 800, height: 300 });
  assert.equal(terminalSelectionPointFromHost(host), null, "missing selection paint returns null");
}

{
  const host = document.createElement("div");
  mockRect(host, { left: 100, top: 200, right: 900, bottom: 500, width: 800, height: 300 });
  const layer = document.createElement("div");
  layer.className = "xterm-selection";
  const paint = document.createElement("div");
  mockRect(paint, { left: 140, top: 280, right: 220, bottom: 296, width: 80, height: 16 });
  layer.appendChild(paint);
  host.appendChild(layer);
  assert.deepEqual(
    terminalSelectionPointFromHost(host),
    { left: 224, top: 302 },
    "toolbar anchors just past the painted selection end inside the terminal",
  );
}

{
  const host = document.createElement("div");
  mockRect(host, { left: 0, top: 0, right: 800, bottom: 300, width: 800, height: 300 });
  const layer = document.createElement("div");
  layer.className = "xterm-selection";
  const spacer = document.createElement("div");
  const paint = document.createElement("div");
  mockRect(spacer, { left: 0, top: 20, right: 800, bottom: 36, width: 800, height: 16 });
  mockRect(paint, { left: 40, top: 36, right: 120, bottom: 52, width: 80, height: 16 });
  layer.append(spacer, paint);
  host.appendChild(layer);
  assert.deepEqual(
    terminalSelectionPointFromHost(host),
    { left: 124, top: 58 },
    "near-full-width spacer paints are ignored so the toolbar stays by the real selection",
  );
}

{
  const host = document.createElement("div");
  mockRect(host, { left: 50, top: 80, right: 450, bottom: 280, width: 400, height: 200 });
  assert.deepEqual(
    clampTerminalSelectionPointToHost({ left: 10, top: 10 }, host, 160, 40),
    { left: 58, top: 88 },
    "points outside the terminal clamp to the panel inset",
  );
  assert.deepEqual(
    clampTerminalSelectionPointToHost({ left: 900, top: 900 }, host, 160, 40),
    { left: 282, top: 232 },
    "points past the terminal bottom-right clamp inside the panel",
  );
}

assert.equal(
  normalizeTerminalSelectionText("  \u001b[31merror\u001b[0m\r\nfailed  "),
  "error\nfailed",
  "selection text strips ANSI and trims",
);
assert.equal(normalizeTerminalSelectionText("\u001b[2J\u001b[H"), "", "control-only selection is empty");

console.log("terminal-selection: ok");
