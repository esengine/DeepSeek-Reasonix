import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { installDesktopHostStub } from "./desktopHostStub";
import { useExtensionSurface } from "../app-runtime/useExtensionSurface";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost" });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, IS_REACT_ACT_ENVIRONMENT: true });
const calls: Array<{ generation: number; values: Record<string, unknown> }> = [];
const errors: string[] = [];
const stub = installDesktopHostStub({ SubmitExtensionFormExact: async (target, values) => {
  calls.push({ generation: target.sessionGeneration, values });
} });
let surface!: ReturnType<typeof useExtensionSurface>;
function Probe({ generation }: { generation?: number }) {
  surface = useExtensionSurface({ activeTabId: "A", sessionId: "session-A", sessionGeneration: generation,
    form: { pluginId: "fixture", surfaceId: "form", generation: 1, formInstanceId: "form-1", formInstanceExact: true },
    notifications: [], dismissForm: () => {}, drainNotifications: () => {}, showToast: text => { errors.push(text); } });
  return null;
}
const root = createRoot(document.getElementById("root")!);
try {
  await act(async () => root.render(<Probe generation={0} />));
  await act(async () => { await surface.submitExtensionForm({ value: "fixture" }); });
  await act(async () => { await surface.cancelExtensionForm(); });
  assert.deepEqual(calls, [{ generation: 0, values: { value: "fixture" } }, { generation: 0, values: { cancelled: true } }]);
  assert.equal(errors.length, 0, "the first session can submit and cancel forms");
  await act(async () => root.render(<Probe />));
  await act(async () => { await surface.submitExtensionForm({ value: "stale" }); });
  await act(async () => { await surface.cancelExtensionForm(); });
  assert.equal(calls.length, 2, "an absent generation is never synthesized as the initial generation");
  assert.equal(errors.length, 1);
  console.log("extension initial binding: generation zero allowed; missing binding fenced");
} finally {
  await act(async () => root.unmount()); stub.uninstall(); dom.window.close();
}
