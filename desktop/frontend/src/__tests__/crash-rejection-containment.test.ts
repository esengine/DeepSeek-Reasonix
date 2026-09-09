// Run: tsx src/__tests__/crash-rejection-containment.test.ts
//
// A rejected Wails call arrives as a bare string (or a stackless object). That
// is an ordinary backend error and must never paint the full-screen crash
// overlay; only rejections carrying a real stack keep the crash surface.

import { JSDOM } from "jsdom";

const dom = new JSDOM("<!doctype html><html><body></body></html>", { url: "http://localhost/" });
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.Event = dom.window.Event;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });

const { installGlobalCrashHandlers, isRecoverableRejectionReason, onRecoverableError } = await import("../lib/globalCrashHandlers");
const { setFrontendDiagnosticSink } = await import("../lib/frontendDiagnosticBridge");

let passed = 0;
let failed = 0;

function ok(cond: boolean, label: string) {
  if (cond) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const diagnostics: string[] = [];
setFrontendDiagnosticSink((_source, type) => { diagnostics.push(type); });
const toasts: string[] = [];
onRecoverableError(({ message }) => { toasts.push(message); });
const originalConsoleError = console.error;
console.error = () => {};

// Mirrors main.tsx: bridge-level Wails filters are installed before the crash handlers.
window.addEventListener("unhandledrejection", (e) => { if (e.reason === "bridge-filtered") e.preventDefault(); });
installGlobalCrashHandlers();

function rejectWith(reason: unknown): Event {
  const event = new dom.window.Event("unhandledrejection", { cancelable: true });
  Object.defineProperty(event, "reason", { value: reason });
  window.dispatchEvent(event);
  return event;
}
const overlay = () => document.getElementById("crash-overlay");

console.log("\ncrash rejection containment");

const stacked = new TypeError("renderer fault");
stacked.stack = "TypeError: renderer fault\n    at render (src/App.tsx:12:3)";
ok(isRecoverableRejectionReason("selected branch is outside the recovery lineage"), "a bare Go error string is recoverable");
ok(isRecoverableRejectionReason({ message: "stackless error-like object" }), "a stackless error-like object is recoverable");
ok(isRecoverableRejectionReason(undefined), "an undefined reason is recoverable");
ok(!isRecoverableRejectionReason(stacked), "an Error with a stack is a genuine crash");

const backend = rejectWith("selected branch is outside the recovery lineage");
ok(backend.defaultPrevented, "a backend rejection is marked handled");
ok(overlay() === null, "a backend rejection does not paint the crash overlay");
ok(
  toasts.length === 1 && toasts[0] === "selected branch is outside the recovery lineage",
  "a backend rejection dispatches the recoverable-error event with its message",
);
ok(diagnostics.includes("unhandled-backend-rejection"), "a backend rejection records a frontend diagnostic");

rejectWith('remote tab "remote-1" status was superseded by newer runtime state');
ok(overlay() === null && toasts.length === 1, "existing crash suppressions still short-circuit before containment");

rejectWith("bridge-filtered");
ok(overlay() === null && toasts.length === 1, "a rejection already handled by a bridge filter is left alone");

const crash = rejectWith(stacked);
ok(!crash.defaultPrevented, "a stacked Error rejection stays unhandled for the crash surface");
ok((overlay()?.textContent ?? "").includes("renderer fault"), "a stacked Error rejection still paints the crash overlay");
ok(toasts.length === 1, "a stacked Error rejection is not downgraded to a toast");

overlay()?.remove();
window.dispatchEvent(new dom.window.ErrorEvent("error", { message: "renderer fault", error: stacked, cancelable: true }));
ok((overlay()?.textContent ?? "").includes("renderer fault"), "window.error still paints the crash overlay");

console.error = originalConsoleError;
dom.window.close();
console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
