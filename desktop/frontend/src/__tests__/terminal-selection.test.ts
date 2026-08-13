import assert from "node:assert/strict";
import { JSDOM } from "jsdom";

const dom = new JSDOM("<!doctype html><html><body></body></html>");
const previousWindow = globalThis.window;
globalThis.window = dom.window as unknown as Window & typeof globalThis;

const {
  clampTerminalSelectionPointToHost,
  handleTerminalCopyKey,
  normalizeTerminalSelectionText,
  readTerminalClipboardText,
  terminalSelectionPointFromHost,
} = await import("../lib/terminalSelection");

function mockRect(
  target: Element,
  rect: { left: number; top: number; right: number; bottom: number; width: number; height: number },
): void {
  Object.defineProperty(target, "getBoundingClientRect", { configurable: true, value: () => rect });
}

function keyEvent(overrides: Partial<ConstructorParameters<typeof KeyboardEvent>[1]> & { key: string }) {
  return { key: overrides.key, ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, ...overrides };
}

// handleTerminalCopyKey: Ctrl+C copies a live selection and swallows the chord;
// without a selection it stays SIGINT (the PTY sees the key).
{
  assert.deepEqual(
    handleTerminalCopyKey({ ...keyEvent({ key: "c", ctrlKey: true }), hasSelection: () => true, getSelection: () => " ls\n" }),
    { intercepted: true, text: "ls" },
    "Ctrl+C with selection copies and intercepts",
  );
  assert.deepEqual(
    handleTerminalCopyKey({ ...keyEvent({ key: "c", ctrlKey: true }), hasSelection: () => false, getSelection: () => "" }),
    { intercepted: false, text: "" },
    "Ctrl+C without selection passes through (SIGINT)",
  );
  assert.deepEqual(
    handleTerminalCopyKey({ ...keyEvent({ key: "c", ctrlKey: true, shiftKey: true }), hasSelection: () => true, getSelection: () => "err" }),
    { intercepted: true, text: "err" },
    "Ctrl+Shift+C with selection copies",
  );
  assert.deepEqual(
    handleTerminalCopyKey({ ...keyEvent({ key: "c", metaKey: true }), hasSelection: () => true, getSelection: () => "x" }),
    { intercepted: true, text: "x" },
    "Cmd+C (macOS) with selection copies",
  );
  assert.deepEqual(
    handleTerminalCopyKey({ ...keyEvent({ key: "c", ctrlKey: true, altKey: true }), hasSelection: () => true, getSelection: () => "x" }),
    { intercepted: false, text: "" },
    "Ctrl+Alt+C is not a copy chord",
  );
  assert.deepEqual(
    handleTerminalCopyKey({ ...keyEvent({ key: "v", ctrlKey: true }), hasSelection: () => true, getSelection: () => "x" }),
    { intercepted: false, text: "" },
    "Ctrl+V is not a copy chord",
  );
  assert.deepEqual(
    handleTerminalCopyKey({ ...keyEvent({ key: "c", ctrlKey: true }), hasSelection: () => true, getSelection: () => "  " }),
    { intercepted: false, text: "" },
    "blank selection text passes through",
  );
  assert.deepEqual(
    handleTerminalCopyKey({ ...keyEvent({ key: "c", ctrlKey: true }), hasSelection: () => true, getSelection: () => " \u001b[31merror\u001b[0m " }),
    { intercepted: true, text: "error" },
    "Ctrl+C selection text is normalized the same as the context-menu path",
  );
}

// normalizeTerminalSelectionText strips ANSI escapes and trims.
{
  assert.equal(normalizeTerminalSelectionText("  \u001b[31merror\u001b[0m\r\nfailed  "), "error\nfailed", "strips ANSI and trims");
  assert.equal(normalizeTerminalSelectionText("\u001b[2J\u001b[H"), "", "control-only selection is empty");
}

// terminalSelectionPointFromHost anchors the toolbar to the painted selection.
{
  const host = dom.window.document.createElement("div");
  mockRect(host, { left: 0, top: 0, right: 800, bottom: 300, width: 800, height: 300 });
  assert.equal(terminalSelectionPointFromHost(host), null, "missing selection paint returns null");
}
{
  const host = dom.window.document.createElement("div");
  mockRect(host, { left: 100, top: 200, right: 900, bottom: 500, width: 800, height: 300 });
  const layer = dom.window.document.createElement("div");
  layer.className = "xterm-selection";
  const paint = dom.window.document.createElement("div");
  mockRect(paint, { left: 140, top: 280, right: 220, bottom: 296, width: 80, height: 16 });
  layer.appendChild(paint);
  host.appendChild(layer);
  assert.deepEqual(
    terminalSelectionPointFromHost(host),
    { left: 224, top: 302 },
    "anchors just past the painted selection end inside the terminal",
  );
}
{
  const host = dom.window.document.createElement("div");
  mockRect(host, { left: 0, top: 0, right: 800, bottom: 300, width: 800, height: 300 });
  const layer = dom.window.document.createElement("div");
  layer.className = "xterm-selection";
  const spacer = dom.window.document.createElement("div");
  const paint = dom.window.document.createElement("div");
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

// clampTerminalSelectionPointToHost keeps the toolbar inside the panel.
{
  const host = dom.window.document.createElement("div");
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

// readTerminalClipboardText prefers the async Clipboard API and falls back to
// the Wails runtime bridge when the webview denies permission.
{
  const originalClipboard = Object.getOwnPropertyDescriptor(globalThis.navigator, "clipboard");
  const originalRuntime = (globalThis.window as unknown as { runtime?: unknown }).runtime;
  try {
    Object.defineProperty(globalThis.navigator, "clipboard", {
      configurable: true,
      value: { readText: async () => "from-navigator" },
    });
    assert.equal(await readTerminalClipboardText(), "from-navigator", "Clipboard API is preferred");

    Object.defineProperty(globalThis.navigator, "clipboard", {
      configurable: true,
      value: { readText: async () => { throw new Error("denied"); } },
    });
    (globalThis.window as unknown as { runtime?: unknown }).runtime = {
      ClipboardGetText: async () => "from-runtime",
    };
    assert.equal(await readTerminalClipboardText(), "from-runtime", "bridge is the permission fallback");

    (globalThis.window as unknown as { runtime?: unknown }).runtime = undefined;
    assert.equal(await readTerminalClipboardText(), "", "no clipboard source returns empty");
  } finally {
    if (originalClipboard) Object.defineProperty(globalThis.navigator, "clipboard", originalClipboard);
    else delete (globalThis.navigator as unknown as Record<string, unknown>).clipboard;
    (globalThis.window as unknown as { runtime?: unknown }).runtime = originalRuntime;
    if (previousWindow === undefined) delete (globalThis as unknown as Record<string, unknown>).window;
    else globalThis.window = previousWindow;
  }
}

console.log("terminal-selection: ok");
