import React, { act, useLayoutEffect } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import assert from "node:assert/strict";
import { useNavigationSurface } from "../lib/useNavigationSurface";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
let surface!: ReturnType<typeof useNavigationSurface>;
function Probe({ tab, session }: { tab: string; session: string }) {
  const next = useNavigationSurface({ activeTabId: tab, sessionKey: session, ready: true,
    backendActivationPending: false, hydrating: false });
  useLayoutEffect(() => { surface = next; });
  return null;
}

try {
  await act(async () => root.render(<Probe tab="a" session="a:1" />));
  act(() => { surface.begin(1); });
  act(() => { surface.maskTarget(1); });
  const firstToken = surface.surfaceCommitToken!;
  assert.ok(firstToken);
  await act(async () => root.render(<Probe tab="a" session="a:2" />));
  const secondToken = surface.surfaceCommitToken!;
  assert.notEqual(secondToken, firstToken, "replacement within the same intent receives a distinct paint receipt");
  act(() => { assert.equal(surface.commitPaint(firstToken, "ready"), null); });
  let receipt: unknown;
  act(() => { receipt = surface.commitPaint(secondToken, "ready"); });
  assert.deepEqual(receipt, { token: secondToken, intent: 1, targetTabId: "a", targetSessionKey: "a:2" });
  act(() => { assert.equal(surface.commitPaint(secondToken, "ready"), null, "a receipt commits once"); });
  const retained = surface.begin;
  act(() => { root.unmount(); retained(2); });
  console.log("PASS navigation receipts are unique, source-bound, and consumed exactly once");
} finally {
  dom.window.close();
}
