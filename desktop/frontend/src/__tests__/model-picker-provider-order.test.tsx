import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { ModelPicker } from "../components/SettingsPanel";
import { LocaleProvider } from "../lib/i18n";
import { writeProviderOrder } from "../lib/providerOrder";
import type { SettingsView } from "../lib/types";

const dom = new JSDOM('<div id="root"></div>', { url: "http://localhost", pretendToBeVisual: true });
Object.assign(globalThis, {
  window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage,
  HTMLElement: dom.window.HTMLElement, Node: dom.window.Node,
  requestAnimationFrame: dom.window.requestAnimationFrame.bind(dom.window),
  cancelAnimationFrame: dom.window.cancelAnimationFrame.bind(dom.window),
  IS_REACT_ACT_ENVIRONMENT: true,
});
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });
Object.defineProperty(dom.window, "matchMedia", { configurable: true, value: () => ({
  matches: true, addEventListener() {}, removeEventListener() {},
}) });

const settings = { providers: [
  { name: "older", displayName: "Older", models: ["one", "two"], keySet: true, apiKeyEnv: "OLDER_KEY" },
  { name: "newer", displayName: "Newer", models: ["three"], keySet: true, apiKeyEnv: "NEWER_KEY" },
] } as SettingsView;
const root = createRoot(document.getElementById("root")!);
await act(async () => root.render(<LocaleProvider><ModelPicker
  s={settings} refs={["older/one", "older/two", "newer/three"]} value="older/one"
  disabled={false} onPick={() => {}}
/></LocaleProvider>));

const options = async () => {
  await act(async () => (document.querySelector("button.settings-select") as HTMLButtonElement).click());
  const values = Array.from(document.querySelectorAll<HTMLElement>('.settings-select-menu [role="option"]'), (item) => item.dataset.value);
  await act(async () => (document.querySelector("button.settings-select") as HTMLButtonElement).click());
  return values;
};
assert.deepEqual(await options(), ["older/one", "older/two", "newer/three"]);
await act(async () => { writeProviderOrder(["newer", "older"]); });
assert.deepEqual(await options(), ["newer/three", "older/one", "older/two"]);
await act(async () => root.unmount());
console.log("settings model picker follows saved provider order: PASS");
