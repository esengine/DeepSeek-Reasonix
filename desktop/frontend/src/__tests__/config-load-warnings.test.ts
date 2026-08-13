// Run: tsx src/__tests__/config-load-warnings.test.ts

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import type { AppBindings } from "../lib/bridge";
import {
  configLoadWarningsKey,
  normalizeConfigLoadWarnings,
  normalizeConfigLoadWarningsEvent,
  normalizeConfigLoadWarningsRevision,
  subscribeConfigLoadWarnings,
  useConfigLoadWarnings,
} from "../lib/useConfigLoadWarnings";

let passed = 0;
let failed = 0;

function equal(actual: unknown, expected: unknown, label: string) {
  if (JSON.stringify(actual) === JSON.stringify(expected)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

console.log("\nconfig load warnings");

equal(
  normalizeConfigLoadWarnings([" warning one ", "", 42, "warning one", "warning two"]),
  ["warning one", "warning two"],
  "Wails payload normalization keeps unique non-empty strings",
);
equal(configLoadWarningsKey(["a", "b"]), '["a","b"]', "warning fingerprints are stable");
equal(configLoadWarningsKey([]), "", "empty warning lists have no fingerprint");
equal(normalizeConfigLoadWarningsRevision(7), 7, "safe event revisions are preserved");
equal(normalizeConfigLoadWarningsRevision(Number.MAX_SAFE_INTEGER + 1), 0, "unsafe event revisions fall back safely");
equal(
  normalizeConfigLoadWarningsEvent({ warnings: [" current warning ", null, "current warning"], revision: 7 }),
  { warnings: ["current warning"], revision: 7 },
  "versioned Wails event payloads are normalized",
);
equal(
  normalizeConfigLoadWarningsEvent(["versioned warning"], 8),
  { warnings: ["versioned warning"], revision: 8 },
  "array event payloads accept an additive revision argument",
);

let runtimeHandler: ((...payload: unknown[]) => void) | undefined;
let unsubscribed = false;
const runtimeHandlers = new Set<(...payload: unknown[]) => void>();
const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', { url: "http://localhost/" });
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
window.runtime = {
  EventsOn: (name: string, cb: (...data: unknown[]) => void) => {
    if (name === "config:load-warnings") {
      runtimeHandler = cb;
      runtimeHandlers.add(cb);
    }
    return () => {
      runtimeHandlers.delete(cb);
      unsubscribed = true;
    };
  },
  BrowserOpenURL: () => {},
};
window.go = { main: { App: {} as AppBindings } };

function emitWarnings(payload: unknown, revision: number) {
  runtimeHandlers.forEach((handler) => handler(payload, revision));
}

const received: Array<{ warnings: string[]; revision: number }> = [];
const stop = subscribeConfigLoadWarnings((snapshot) => received.push(snapshot));
runtimeHandler?.([" current warning ", null, "current warning"], 7);
runtimeHandler?.([], 8);
equal(received, [{ warnings: ["current warning"], revision: 7 }], "runtime bridge forwards normalized non-empty warnings");
stop();
equal(unsubscribed, true, "runtime warning subscription is disposable");

type WarningHook = ReturnType<typeof useConfigLoadWarnings>;
let warningHook: WarningHook | undefined;
function Probe() {
  warningHook = useConfigLoadWarnings();
  return null;
}

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing test root");
const root = createRoot(rootElement);
await act(async () => { root.render(React.createElement(Probe)); });

await act(async () => { emitWarnings(["runtime warning"], 2); });
await act(async () => { warningHook?.applySnapshot([], 1); });
equal(warningHook?.configLoadWarnings, ["runtime warning"], "stale startup snapshots cannot clear runtime warnings");

await act(async () => { warningHook?.dismiss(); });
await act(async () => { emitWarnings(["runtime warning"], 3); });
equal(warningHook?.configLoadWarnings, [], "dismissed warnings stay hidden across repeated session builds");

await act(async () => { warningHook?.reload([], 4); });
await act(async () => { emitWarnings(["runtime warning"], 3); });
equal(warningHook?.configLoadWarnings, [], "events started before a successful reload cannot revive stale warnings");

await act(async () => { emitWarnings(["runtime warning"], 5); });
equal(warningHook?.configLoadWarnings, ["runtime warning"], "a newer recurrence can reuse the same warning fingerprint");
await act(async () => { root.unmount(); });

if (failed > 0) {
  process.stdout.write(`\n${failed} failed, ${passed} passed\n`);
  process.exit(1);
}
process.stdout.write(`\n${passed} passed\n`);
