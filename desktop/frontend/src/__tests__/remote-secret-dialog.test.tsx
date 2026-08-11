// Run: tsx src/__tests__/remote-secret-dialog.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";

import type { AppBindings } from "../lib/bridge";

let passed = 0;
let failed = 0;
function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\nRemote SSH one-shot credential prompt");
const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
// React's legacy input-event fallback expects these IE hooks when JSDOM does
// not advertise native InputEvent support.
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });

// Import ReactDOM and the component only after installing the DOM so React
// selects its native input-event path instead of the legacy IE polyfill.
const [{ createRoot }, { RemoteSecretDialog }, { LocaleProvider }, { useRemoteStore }] = await Promise.all([
  import("react-dom/client"),
  import("../components/RemoteSecretDialog"),
  import("../lib/i18n"),
  import("../store/remote"),
]);

const calls: Array<{ hostId: string; promptId: string; secret: string; accept: boolean }> = [];
window.go = { main: { App: {
  async ConfirmRemoteSecret(hostId: string, promptId: string, secret: string, accept: boolean) {
    calls.push({ hostId, promptId, secret, accept });
  },
} as Partial<AppBindings> as AppBindings } };

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);
await act(async () => root.render(<LocaleProvider><RemoteSecretDialog /></LocaleProvider>));

await act(async () => {
  useRemoteStore.getState().applyStatus({
    hostId: "box",
    state: "pending_secret",
    secretPrompt: { promptId: "prompt-1", hostId: "box", host: "dev@box.test", kind: "password" },
  });
});
const input = document.querySelector<HTMLInputElement>('input[type="password"]');
ok(Boolean(input), "password request opens a masked input dialog");
ok(document.body.textContent?.includes("dev@box.test") === true, "dialog identifies the SSH target");

await act(async () => {
  if (!input) return;
  const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")?.set;
  setter?.call(input, "one-shot-secret");
  input.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  input.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
  await Promise.resolve();
});
await act(async () => {
  if (!input) return;
  input.closest("form")?.dispatchEvent(new dom.window.Event("submit", { bubbles: true, cancelable: true }));
  await Promise.resolve();
});
ok(calls.length === 1 && calls[0]?.promptId === "prompt-1" && calls[0]?.secret === "one-shot-secret" && calls[0]?.accept === true, "submit sends the prompt ID and secret once to the native bridge");
ok(useRemoteStore.getState().pendingSecretPrompt === null, "resolved prompt is removed from shared UI state");
ok(document.body.textContent?.includes("one-shot-secret") === false, "secret plaintext is never rendered into the page");

await act(async () => root.unmount());
dom.window.close();
process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
