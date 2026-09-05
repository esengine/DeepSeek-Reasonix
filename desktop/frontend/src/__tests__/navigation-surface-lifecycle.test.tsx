import React, { act, useLayoutEffect } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import assert from "node:assert/strict";
import { useNavigationSurface } from "../lib/useNavigationSurface";
import { projectNavigationSurfaceTarget } from "../app-runtime/conversationProjection";
import { initialState } from "../lib/useController";
import type { RemoteSessionApi } from "../lib/useRemoteSession";

const dom = new JSDOM("<div id='root'></div>");
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
let surface!: ReturnType<typeof useNavigationSurface>;
function Probe({ tab, session, remote }: { tab: string; session: string;
  remote?: Pick<RemoteSessionApi, "state" | "hydrated" | "surfaceGeneration" | "error"> }) {
  const next = useNavigationSurface(projectNavigationSurfaceTarget({ activeTabId: tab, sessionKey: session,
    local: { ...initialState, meta: { ...initialState.meta, ready: !remote } as typeof initialState.meta,
      backendActivationPending: Boolean(remote), hydrating: Boolean(remote) }, remote }));
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
  const remote = { state: "ready", hydrated: true, surfaceGeneration: 1, error: "" } as const;
  await act(async () => root.render(<Probe tab="remote" session="workspace" remote={remote} />));
  act(() => { surface.begin(2); surface.maskTarget(2); });
  const remoteToken = surface.surfaceCommitToken!;
  assert.ok(remoteToken, "remote readiness is independent of the inactive local controller's pending hydration");
  await act(async () => root.render(<Probe tab="remote" session="workspace" remote={{ ...remote, surfaceGeneration: 2 }} />));
  act(() => { assert.equal(surface.commitPaint(remoteToken, "ready"), null, "a former Serve generation cannot acknowledge the new surface"); });
  act(() => { assert.ok(surface.commitPaint(surface.surfaceCommitToken!, "ready")); });
  assert.equal(surface.transitioning, false, "the same paint transaction releases remote Composer readiness");
  await act(async () => root.render(<Probe tab="remote" session="workspace" remote={{ ...remote, state: "error", hydrated: false, error: "offline" }} />));
  act(() => { surface.begin(3); surface.maskTarget(3); });
  assert.equal(surface.transitioning, false, "remote hydration failure terminates the masked navigation, leaving recovery reachable");
  const retained = surface.begin;
  act(() => { root.unmount(); retained(2); });
  console.log("PASS navigation receipts are unique, source-bound, and consumed exactly once");
} finally {
  dom.window.close();
}
