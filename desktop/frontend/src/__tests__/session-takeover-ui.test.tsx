// Run: tsx src/__tests__/session-takeover-ui.test.tsx

import React from "react";
import { JSDOM } from "jsdom";
import { act } from "react";
import type { AppBindings } from "../lib/bridge";
import { activeLeaseBlockedTab } from "../lib/tabMetaRefresh";
import type { TabMeta } from "../lib/types";

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

console.log("\nSession takeover UI lifecycle");
const tabMetas = [
  { id: "tab-a", remote: false, runtime: { phase: "lease_blocked", epoch: "a", issue: { code: "session_lease_held", message: "held", retryable: true } } },
  { id: "tab-b", remote: false, runtime: { phase: "ready", epoch: "b" } },
] as TabMeta[];
ok(activeLeaseBlockedTab(tabMetas, "tab-b") === undefined, "background lease conflicts do not arm the active tab");
ok(activeLeaseBlockedTab(tabMetas, "tab-a")?.id === "tab-a", "the active lease-blocked tab exposes takeover");
const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });

let rejectTakeover: ((reason?: unknown) => void) | null = null;
window.go = { main: { App: {
  async QuerySessionTakeover() {
    return { available: true, sessionPath: "/session.jsonl", holder: "serve", remoteAttached: true, running: true, mirrored: false };
  },
  TakeoverSession() {
    return new Promise<void>((_resolve, reject) => { rejectTakeover = reject; });
  },
} as Partial<AppBindings> as AppBindings } };

const [{ createRoot }, { LocaleProvider }, { RemoteReclaimBanner }, { SessionTakeoverDialog }] = await Promise.all([
  import("react-dom/client"),
  import("../lib/i18n"),
  import("../components/RemoteReclaimBanner"),
  import("../components/SessionTakeoverDialog"),
]);

const host = document.getElementById("root");
if (!host) throw new Error("missing root");
const root = createRoot(host);
const reclaimed: string[] = [];
const flush = () => new Promise((resolve) => setTimeout(resolve, 20));
const renderBanner = (tabId: string, busyTabId: string | null = null) => act(async () => {
  root.render(<LocaleProvider><RemoteReclaimBanner tabId={tabId} busyTabId={busyTabId} onReclaim={(id) => reclaimed.push(id)} /></LocaleProvider>);
});

await renderBanner("tab-a");
await act(async () => document.querySelector<HTMLButtonElement>(".banner button")?.click());
await renderBanner("tab-b");
await act(async () => document.querySelector<HTMLButtonElement>(".banner button")?.click());
ok(reclaimed.length === 0, "switching A to B requires B's own first confirmation click");
await act(async () => document.querySelector<HTMLButtonElement>(".banner button")?.click());
ok(reclaimed.length === 1 && reclaimed[0] === "tab-b", "second click reclaims only the armed tab");
await renderBanner("tab-b", "tab-a");
ok(document.querySelector<HTMLButtonElement>(".banner button")?.disabled === true, "any reclaim in progress disables the active reclaim button");

let closes = 0;
await act(async () => {
  root.render(<LocaleProvider><SessionTakeoverDialog tabId="tab-a" onClose={() => { closes += 1; }} /></LocaleProvider>);
	await flush();
});
await act(async () => {
	await flush();
});
const waitButton = Array.from(document.querySelectorAll<HTMLButtonElement>(".session-takeover-dialog button"))
	.find((button) => button.classList.contains("btn--primary"));
await act(async () => waitButton?.click());
await act(async () => document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true })));
ok(closes === 0 && document.querySelector(".session-takeover-dialog") !== null, "busy Escape is consumed without closing the takeover dialog");
await act(async () => {
  rejectTakeover?.(new Error("injected takeover failure"));
  await Promise.resolve();
});
ok(document.body.textContent?.includes("injected takeover failure") === true, "action failure remains visible and restores the dialog");
await act(async () => document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true })));
ok(closes === 1, "Escape closes again after a failed action leaves busy state");

await act(async () => root.unmount());
dom.window.close();
process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
