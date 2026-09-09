import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { syncMainWindowMaximised, useWindowsMaximisedSync } from "../app-runtime/useNativeWindowController";
import { useWindowChromeStore } from "../store/windowChrome";

const dom = new JSDOM("<div id='root'></div>", { pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
globalThis.Event = dom.window.Event;
const root = createRoot(document.getElementById("root")!);

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((yes) => { resolve = yes; });
  return { promise, resolve };
}

const bridgeCalls: string[] = [];
let maximisedValue = false;
let maximisedGate: ReturnType<typeof deferred<boolean>> | null = null;
window.go = {
  main: {
    App: {
      IsMainWindowMaximised: async () => {
        bridgeCalls.push("query");
        if (maximisedGate) return maximisedGate.promise;
        return maximisedValue;
      },
    },
  },
} as unknown as typeof window.go;

function Probe({ enabled }: { enabled: boolean }) {
  useWindowsMaximisedSync(enabled);
  return null;
}

const maximised = () => useWindowChromeStore.getState().mainWindowMaximised;

try {
  syncMainWindowMaximised();
  assert.equal(bridgeCalls.length, 0, "sync before any lifecycle is a no-op");

  maximisedValue = true;
  await act(async () => root.render(<Probe enabled={true} />));
  assert.equal(maximised(), true, "the enabling lifecycle syncs the native flag into the store");
  assert.deepEqual(bridgeCalls, ["query"], "the initial sync queries the bridge once");

  maximisedValue = false;
  await act(async () => { window.dispatchEvent(new window.Event("resize")); });
  assert.equal(maximised(), false, "a resize event re-syncs the flag");
  assert.equal(bridgeCalls.length, 2, "listener sync queries the bridge again");

  maximisedValue = true;
  await act(async () => { window.dispatchEvent(new window.Event("focus")); });
  assert.equal(maximised(), true, "a focus event re-syncs the flag");

  const supersededGate = deferred<boolean>();
  maximisedGate = supersededGate;
  await act(async () => { syncMainWindowMaximised(); });
  maximisedGate = null;
  maximisedValue = false;
  await act(async () => { syncMainWindowMaximised(); });
  assert.equal(maximised(), false, "the newer sync lands first");
  await act(async () => { supersededGate.resolve(true); await supersededGate.promise; });
  assert.equal(maximised(), false, "an out-of-order resolution from a superseded sync is discarded");

  await act(async () => root.render(<Probe enabled={false} />));
  assert.equal(maximised(), false, "disabling the lifecycle resets the flag");
  bridgeCalls.length = 0;
  syncMainWindowMaximised();
  assert.equal(bridgeCalls.length, 0, "event-handler sync stays gated while disabled");

  await act(async () => root.unmount());
  bridgeCalls.length = 0;
  syncMainWindowMaximised();
  assert.equal(bridgeCalls.length, 0, "unmounting the host disables the sync gate");

  console.log("windows maximised sync: lifecycle gating, listener sync, generation fence and store ownership passed");
} finally { dom.window.close(); }
