import assert from "node:assert/strict";
import React, { act, StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { StartupGateLifecycle } from "../app-runtime/StartupGateLifecycle";
import { useAppNavigationStore } from "../store/appNavigation";
import { useOverlayStore } from "../store/overlays";
import { installDesktopHostStub } from "./desktopHostStub";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost" });
Object.assign(globalThis, { window: dom.window, document: dom.window.document,
  localStorage: dom.window.localStorage, IS_REACT_ACT_ENVIRONMENT: true });
const pending: Array<(needs: boolean) => void> = [];
installDesktopHostStub({
  NeedsOnboarding: () => new Promise<boolean>((resolve) => { pending.push(resolve); }),
});
async function mount() {
  localStorage.clear();
  useAppNavigationStore.setState({ page: { kind: "workspace" }, generation: 0, settingsFocus: null });
  useOverlayStore.getState().setProviderSetupNeeded(false);
  const root = createRoot(document.getElementById("root")!);
  await act(async () => root.render(<StrictMode><StartupGateLifecycle /></StrictMode>));
  return root;
}
try {
  let root = await mount();
  assert.equal(pending.length, 2);
  await act(async () => pending.shift()!(true));
  assert.equal(useOverlayStore.getState().providerSetupNeeded, false, "retired StrictMode probe cannot publish");
  await act(async () => pending.shift()!(true));
  assert.deepEqual(useAppNavigationStore.getState().settingsFocus, { target: "model-access", onboarding: true });
  assert.deepEqual(useAppNavigationStore.getState().page, { kind: "settings", tab: "providers" });
  await act(async () => root.unmount());

  root = await mount();
  useAppNavigationStore.getState().openPage({ kind: "trash" });
  await act(async () => { pending.splice(0).forEach(resolve => resolve(true)); });
  assert.deepEqual(useAppNavigationStore.getState().page, { kind: "trash" }, "late setup result cannot replace a newer navigation intent");
  assert.equal(useOverlayStore.getState().providerSetupNeeded, true, "current probe still publishes advisory setup status");
  await act(async () => root.unmount());

  root = await mount();
  await act(async () => root.unmount());
  await act(async () => { pending.splice(0).forEach(resolve => resolve(true)); });
  assert.equal(useOverlayStore.getState().providerSetupNeeded, false, "unmounted probe cannot publish setup state");
  assert.deepEqual(useAppNavigationStore.getState().page, { kind: "workspace" });
  console.log("startup gate: direct setup, StrictMode retirement, navigation generation and unmount passed");
} finally { dom.window.close(); }
