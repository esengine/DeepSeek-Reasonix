import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { useOnboardingCommands } from "../app-runtime/useOnboardingCommands";
import { useOverlayStore } from "../store/overlays";
import { useAppNavigationStore } from "../store/appNavigation";
import { onboardingWasDismissed } from "../lib/onboarding";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost" });
Object.assign(globalThis, { window: dom.window, document: dom.window.document,
  localStorage: dom.window.localStorage, IS_REACT_ACT_ENVIRONMENT: true });
const root = createRoot(document.getElementById("root")!);
let commands!: ReturnType<typeof useOnboardingCommands>;
let completed = 0;
function Probe() { commands = useOnboardingCommands(() => { completed++; }); return null; }
await act(async () => root.render(<Probe />));
commands.chooseOnboardingProvider();
assert.deepEqual(useAppNavigationStore.getState().page, { kind: "settings", tab: "models" });
assert.deepEqual(useAppNavigationStore.getState().settingsFocus, { target: "model-access" });
assert.equal(useOverlayStore.getState().needsOnboarding, false);
commands.completeOnboarding();
assert.equal(completed, 1);
commands.skipOnboarding();
assert.equal(onboardingWasDismissed(), true);
await act(async () => root.unmount());
commands.completeOnboarding();
assert.equal(completed, 1, "unmounted owner cannot publish onboarding state");
dom.window.close();
console.log("onboarding commands: model access, completion, dismissal and disposal passed");
