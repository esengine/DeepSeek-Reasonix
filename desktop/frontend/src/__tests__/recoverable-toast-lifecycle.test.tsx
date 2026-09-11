import assert from "node:assert/strict";
import React, { act, StrictMode, useLayoutEffect } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useRecoverableErrorToasts } from "../app-runtime/useAppEffectHosts";
import { RECOVERABLE_ERROR_EVENT } from "../lib/globalCrashHandlers";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document,
  Node: dom.window.Node, HTMLElement: dom.window.HTMLElement, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
const delivered: string[] = [];
const fire = (message: string) => window.dispatchEvent(new dom.window.CustomEvent(RECOVERABLE_ERROR_EVENT, { detail: { message } }));
function Fixture({ owner }: { owner: string }) {
  useRecoverableErrorToasts((message, level, options) => {
    assert.equal(level, "warn"); assert.equal(options?.durationMs, 6000);
    delivered.push(`${owner}:${message}`);
  });
  useLayoutEffect(() => () => { fire("layout-cleanup"); }, []);
  return null;
}
try {
  for (let index = 0; index < 32; index++) {
    await act(async () => root.render(<StrictMode><Fixture owner={String(index)} /></StrictMode>));
    await act(async () => { fire("backend-error"); });
    assert.equal(delivered.length, index + 1, "one subscription survives StrictMode and callback replacement");
    assert.equal(delivered.at(-1), `${index}:backend-error`, "delivery uses the latest committed toast sink");
  }
  await act(async () => root.unmount());
  fire("after-unmount");
  assert.equal(delivered.length, 32, "layout teardown revokes delivery before passive unsubscribe, with no late toast");
  console.log("recoverable toast lifecycle: current sink, single subscription and synchronous teardown passed");
} finally { dom.window.close(); }
