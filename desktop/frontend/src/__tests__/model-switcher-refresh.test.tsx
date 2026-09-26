// Run: tsx src/__tests__/model-switcher-refresh.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { ModelSwitcher, normalizeModelInfo } from "../components/ModelSwitcher";
import { LocaleProvider } from "../lib/i18n";
import { writeProviderOrder } from "../lib/providerOrder";
import type { ModelInfo } from "../lib/types";
import { installDesktopHostStub } from "./desktopHostStub";

class TestResizeObserver {
  observe() {}
  disconnect() {}
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
dom.window.localStorage.clear();
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.localStorage = dom.window.localStorage;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.HTMLInputElement = dom.window.HTMLInputElement;
globalThis.Node = dom.window.Node;
globalThis.ResizeObserver = TestResizeObserver as unknown as typeof ResizeObserver;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });
Object.defineProperty(window, "matchMedia", {
  configurable: true,
  value: () => ({
    matches: true,
    media: "(prefers-reduced-motion: reduce)",
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent: () => false,
  }),
});
const { createRoot } = await import("react-dom/client");

const stale = deferred<ModelInfo[]>();
const fresh = deferred<ModelInfo[]>();
let calls = 0;
const picked: string[] = [];
const pickGates: Array<ReturnType<typeof deferred<boolean>>> = [];
let catalogLoader: (() => Promise<ModelInfo[]>) | undefined;
const normalizedNullMetadata = normalizeModelInfo({
  ref: "provider-a/model-a",
  provider: null,
  model: null,
  current: true,
} as unknown as ModelInfo);
if (normalizedNullMetadata.provider !== "" || normalizedNullMetadata.model !== "") {
  throw new Error(`null model metadata was not normalized: ${JSON.stringify(normalizedNullMetadata)}`);
}
let currentCatalog: ModelInfo[] = [
  { ref: "glm-cn/glm-5.2", provider: "glm-cn", model: "glm-5.2", current: true },
];
installDesktopHostStub(({
  main: {
    App: {
      ModelsForTab: async () => {
        calls += 1;
        if (calls === 1) return stale.promise;
        if (calls === 2) return fresh.promise;
        if (catalogLoader) return catalogLoader();
        return currentCatalog;
      },
    },
  },
}).main.App);

const root = createRoot(document.getElementById("root")!);
const renderSwitcher = (label: string, tabId: string, ready = true, sessionKey?: string, disabled = false, dismissSignal?: number) => (
  <LocaleProvider>
    <ModelSwitcher
      label={label}
      tabId={tabId}
      ready={ready}
      sessionKey={sessionKey}
      disabled={disabled}
      dismissSignal={dismissSignal}
      onPick={(ref) => {
        picked.push(ref);
        return pickGates.shift()?.promise ?? Promise.resolve(true);
      }}
    />
  </LocaleProvider>
);
await act(async () => {
  root.render(renderSwitcher("deepseek-v4-flash", "tab-a"));
});

await act(async () => {
  window.dispatchEvent(new Event("reasonix:model-catalog-changed"));
  fresh.resolve([{ ref: "glm-cn/glm-5.2", provider: "glm-cn", model: "glm-5.2", current: true }]);
  await fresh.promise;
});
await act(async () => {
  stale.resolve([{ ref: "deepseek/deepseek-v4-flash", provider: "deepseek", model: "deepseek-v4-flash", current: true }]);
  await stale.promise;
});
await act(async () => {
  (document.querySelector(".modelsw__trigger") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});

const options = Array.from(document.querySelectorAll<HTMLElement>("[role='option']")).map((item) => item.textContent?.trim());
if (options.length !== 1 || !options[0]?.includes("glm-5.2") || !options[0]?.includes("glm-cn")) {
  throw new Error(`model catalog did not keep the fresh result: ${JSON.stringify(options)}`);
}
if (calls < 3) throw new Error(`expected mount, settings refresh, and open loads; got ${calls}`);

await act(async () => {
  (document.querySelector("[role='option'][aria-selected='true']") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
if (picked.length !== 0) throw new Error(`current model should be a no-op, picked ${JSON.stringify(picked)}`);
if ((document.querySelector(".modelsw__trigger") as HTMLButtonElement).getAttribute("aria-expanded") !== "false") {
  throw new Error("current-model no-op did not close the menu");
}

currentCatalog = [
  { ref: "glm-cn/glm-5.2", provider: "glm-cn", model: "glm-5.2", current: true },
  { ref: "deepseek/deepseek-v4-flash", provider: "deepseek", model: "deepseek-v4-flash", current: false },
];
const pendingPickGate = deferred<boolean>();
pickGates.push(pendingPickGate, pendingPickGate);
await act(async () => {
  (document.querySelector(".modelsw__trigger") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
await act(async () => {
  await new Promise((resolve) => setTimeout(resolve, 0));
});
const next = Array.from(document.querySelectorAll<HTMLButtonElement>("[role='option']"))
  .find((option) => option.textContent?.includes("deepseek-v4-flash"));
if (!next) throw new Error("pending-switch model option did not load");
await act(async () => {
  next.click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
if (picked[0] !== "deepseek/deepseek-v4-flash") {
  throw new Error(`non-current model was not selected: ${JSON.stringify(picked)}`);
}

// The backend catalog may still identify the outgoing GLM model as current
// while the first switch is pending. Selecting it now must enqueue a rollback.
await act(async () => {
  (document.querySelector(".modelsw__trigger") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
await act(async () => {
  await new Promise((resolve) => setTimeout(resolve, 0));
});
const rollback = document.querySelector<HTMLButtonElement>("[role='option'][aria-selected='true']");
if (!rollback) throw new Error("pending-switch rollback option did not load");
await act(async () => {
  rollback.click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
if (picked[1] !== "glm-cn/glm-5.2") {
  throw new Error(`pending current-model rollback was swallowed: ${JSON.stringify(picked)}`);
}

// Pending work belongs to tab A. Reusing the mounted switcher for tab B must
// not turn B's settled current model into another rollback request.
currentCatalog = [
  { ref: "provider-b/model-b", provider: "provider-b", model: "model-b", current: true },
];
await act(async () => {
  root.render(renderSwitcher("model-b", "tab-b"));
  await new Promise((resolve) => setTimeout(resolve, 0));
});
await act(async () => {
  (document.querySelector(".modelsw__trigger") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
await act(async () => {
  await new Promise((resolve) => setTimeout(resolve, 0));
});
const tabBCurrent = document.querySelector<HTMLButtonElement>("[role='option'][aria-selected='true']");
if (!tabBCurrent) throw new Error("tab B current model did not load");
await act(async () => {
  tabBCurrent.click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
if (picked.length !== 2) {
  throw new Error(`tab A pending picks leaked into tab B: ${JSON.stringify(picked)}`);
}

await act(async () => {
  pendingPickGate.resolve(true);
  await pendingPickGate.promise;
});

// A failed latest pick must immediately roll back its optimistic selection,
// then reconcile from the authoritative backend catalog. Retrying the same
// target before that catalog request completes must remain possible.
currentCatalog = [
  { ref: "provider-a/model-a", provider: "provider-a", model: "model-a", current: true },
  { ref: "provider-b/model-b", provider: "provider-b", model: "model-b", current: false },
];
await act(async () => {
  root.render(renderSwitcher("model-a", "tab-c"));
  await new Promise((resolve) => setTimeout(resolve, 0));
});
await act(async () => {
  (document.querySelector(".modelsw__trigger") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
const failedTarget = Array.from(document.querySelectorAll<HTMLButtonElement>("[role='option']"))
  .find((option) => option.textContent?.includes("model-b"));
if (!failedTarget) throw new Error("failed-switch target did not load");
const failedPickGate = deferred<boolean>();
pickGates.push(failedPickGate);
const catalogReloadGate = deferred<ModelInfo[]>();
catalogLoader = () => catalogReloadGate.promise;
await act(async () => {
  failedTarget.click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
failedPickGate.resolve(false);
await act(async () => {
  await failedPickGate.promise;
  await new Promise((resolve) => setTimeout(resolve, 0));
});
await act(async () => {
  (document.querySelector(".modelsw__trigger") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
const rolledBackCurrent = document.querySelector<HTMLButtonElement>("[role='option'][aria-selected='true']");
if (!rolledBackCurrent?.textContent?.includes("model-a")) {
  throw new Error(`failed pick did not immediately roll back: ${rolledBackCurrent?.textContent ?? "missing"}`);
}
const retryTarget = Array.from(document.querySelectorAll<HTMLButtonElement>("[role='option']"))
  .find((option) => option.textContent?.includes("model-b"));
if (!retryTarget) throw new Error("failed-switch target was not available for immediate retry");
const retryPickGate = deferred<boolean>();
pickGates.push(retryPickGate);
await act(async () => {
  retryTarget.click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
if (picked.filter((ref) => ref === "provider-b/model-b").length !== 2) {
  throw new Error(`failed target could not be retried: ${JSON.stringify(picked)}`);
}

// The failed attempt's catalog request describes the pre-retry backend state.
// Its late completion must not overwrite the newer optimistic retry.
catalogReloadGate.resolve(currentCatalog);
await act(async () => {
  await catalogReloadGate.promise;
  await new Promise((resolve) => setTimeout(resolve, 0));
});
const retryTriggerLabel = (document.querySelector(".modelsw__trigger") as HTMLButtonElement)
  .getAttribute("aria-label") ?? "";
if (!retryTriggerLabel.includes("provider-b")) {
  throw new Error(`stale failure reconciliation overwrote the retry: ${retryTriggerLabel}`);
}
retryPickGate.resolve(true);
await act(async () => {
  await retryPickGate.promise;
  await new Promise((resolve) => setTimeout(resolve, 0));
});
catalogLoader = undefined;

// An older superseded failure also must not roll back a newer pending pick.
currentCatalog = [
  { ref: "provider-a/model-a", provider: "provider-a", model: "model-a", current: true },
  { ref: "provider-b/model-b", provider: "provider-b", model: "model-b", current: false },
  { ref: "provider-c/model-c", provider: "provider-c", model: "model-c", current: false },
];
await act(async () => {
  root.render(renderSwitcher("model-a", "tab-d"));
  await new Promise((resolve) => setTimeout(resolve, 0));
});
await act(async () => {
  (document.querySelector(".modelsw__trigger") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
const olderTarget = Array.from(document.querySelectorAll<HTMLButtonElement>("[role='option']"))
  .find((option) => option.textContent?.includes("model-b"));
if (!olderTarget) throw new Error("older switch target did not load");
const olderPickGate = deferred<boolean>();
pickGates.push(olderPickGate);
await act(async () => {
  olderTarget.click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
await act(async () => {
  (document.querySelector(".modelsw__trigger") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
const newerTarget = Array.from(document.querySelectorAll<HTMLButtonElement>("[role='option']"))
  .find((option) => option.textContent?.includes("model-c"));
if (!newerTarget) throw new Error("newer switch target did not load");
const newerPickGate = deferred<boolean>();
pickGates.push(newerPickGate);
await act(async () => {
  newerTarget.click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
olderPickGate.resolve(false);
await act(async () => {
  await olderPickGate.promise;
  await new Promise((resolve) => setTimeout(resolve, 0));
});
const newerTriggerLabel = (document.querySelector(".modelsw__trigger") as HTMLButtonElement)
  .getAttribute("aria-label") ?? "";
if (!newerTriggerLabel.includes("provider-c")) {
  throw new Error(`superseded failure rolled back the newer pick: ${newerTriggerLabel}`);
}
newerPickGate.resolve(true);
await act(async () => {
  await newerPickGate.promise;
});

// Legacy or malformed settings can surface null display metadata despite the
// bridge contract. Catalog loading must normalize it before grouping/search.
currentCatalog = [
  { ref: "provider-a/model-a", provider: null, model: null, current: true } as unknown as ModelInfo,
  { ref: "provider-b/model-b", provider: "provider-b", model: "model-b", current: false },
];
await act(async () => {
  root.render(renderSwitcher("model-a", "tab-null-metadata"));
  await new Promise((resolve) => setTimeout(resolve, 0));
});
await act(async () => {
  (document.querySelector(".modelsw__trigger") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
const nullSafeOptions = Array.from(document.querySelectorAll<HTMLElement>("[role='option']"));
if (nullSafeOptions.length !== 2) {
  throw new Error(`null model metadata prevented catalog rendering: ${nullSafeOptions.length}`);
}

// Connection order and duplicate model IDs must survive selection and renaming.
currentCatalog = [
  { ref: "z/shared", provider: "z", displayName: "First connection", model: "shared", current: false },
  { ref: "a/shared", provider: "a", displayName: "Second connection", model: "shared", current: true },
];
await act(async () => {
  window.dispatchEvent(new Event("reasonix:model-catalog-changed"));
  await new Promise(resolve => setTimeout(resolve, 0));
});
const connectionLabels = Array.from(document.querySelectorAll(".modelsw__meta"), el => el.textContent);
if (connectionLabels.join("|") !== "First connection|Second connection · Current model") {
  throw new Error(`connection labels or catalog order differ: ${connectionLabels}`);
}
if (document.querySelectorAll("[role='option']").length !== 2) {
  throw new Error("same model ID in separate connections was merged");
}

// Favorites are keyed by the full provider/model ref, remain in the open menu,
// and can be used as a rail filter without switching the active model.
const firstFavorite = document.querySelector<HTMLButtonElement>(".modelsw__favorite");
if (!firstFavorite) throw new Error("favorite control was not rendered");
const pickedBeforeFavorite = picked.length;
await act(async () => { firstFavorite.click(); });
if (picked.length !== pickedBeforeFavorite) throw new Error("favorite action triggered a model switch");
if (localStorage.getItem("reasonix-model-favorites-v1") !== JSON.stringify(["z/shared"])) {
  throw new Error(`favorite ref was not persisted: ${localStorage.getItem("reasonix-model-favorites-v1")}`);
}
const favoriteFilter = document.querySelector<HTMLButtonElement>('.modelsw__rail-item[aria-label="Favorites"]');
if (!favoriteFilter) throw new Error("favorites rail filter was not rendered");
await act(async () => { favoriteFilter.click(); });
const favoriteOptions = Array.from(document.querySelectorAll<HTMLElement>("[role='option']"));
if (favoriteOptions.length !== 1 || !favoriteOptions[0]?.textContent?.includes("First connection")) {
  throw new Error(`favorites filter did not isolate the saved connection: ${favoriteOptions.map(el => el.textContent)}`);
}
const allFilter = document.querySelector<HTMLButtonElement>('.modelsw__rail-item[aria-label="All models"]');
await act(async () => { allFilter?.click(); });
await act(async () => { document.querySelector<HTMLButtonElement>(".modelsw__favorite")?.click(); });
await act(async () => { writeProviderOrder(["a", "z"]); });
const orderedRail = Array.from(document.querySelectorAll<HTMLButtonElement>(".modelsw__rail-item"))
  .map((item) => item.getAttribute("aria-label"));
if (orderedRail.slice(2).join("|") !== "Second connection|First connection") {
  throw new Error(`provider rail did not apply saved order: ${orderedRail}`);
}
const orderedOptions = Array.from(document.querySelectorAll<HTMLElement>("[role='option']"));
if (!orderedOptions[0]?.textContent?.includes("Second connection") || !orderedOptions[1]?.textContent?.includes("First connection")) {
  throw new Error(`model list did not follow provider order: ${orderedOptions.map((item) => item.textContent)}`);
}
if (orderedOptions[0]?.getAttribute("aria-selected") !== "true") {
  throw new Error("reordering changed the active model");
}

// Search is catalog-wide even when the user was browsing one provider. The
// top-level field moves back to All models before applying its keyword.
currentCatalog = [
  { ref: "deepseek/deepseek-v4-flash", provider: "deepseek", displayName: "DeepSeek Official", model: "deepseek-v4-flash", current: true },
  { ref: "moonshot/kimi-k2", provider: "moonshot", displayName: "Moonshot", model: "kimi-k2", current: false },
];
await act(async () => {
  window.dispatchEvent(new Event("reasonix:model-catalog-changed"));
  await new Promise(resolve => setTimeout(resolve, 0));
});
const deepseekFilter = document.querySelector<HTMLButtonElement>('.modelsw__rail-item[aria-label="DeepSeek Official"]');
if (!deepseekFilter) throw new Error("DeepSeek provider filter was not rendered");
await act(async () => { deepseekFilter.click(); });
if (document.querySelectorAll("[role='option']").length !== 1) {
  throw new Error("provider browsing did not narrow the model catalog");
}
const catalogSearch = document.querySelector<HTMLInputElement>(".modelsw__search-input");
if (!catalogSearch) throw new Error("catalog search input was not rendered");
await act(async () => {
  Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "value")!.set!.call(catalogSearch, "kimi");
  catalogSearch.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  await new Promise(resolve => setTimeout(resolve, 0));
});
const globalSearchOptions = Array.from(document.querySelectorAll<HTMLElement>("[role='option']"));
if (globalSearchOptions.length !== 1 || !globalSearchOptions[0]?.textContent?.includes("kimi-k2")) {
  throw new Error(`provider filter constrained global search: ${globalSearchOptions.map(item => item.textContent)}`);
}
if (allFilter?.getAttribute("aria-pressed") !== "true") {
  throw new Error("global search did not activate the All models rail state");
}

currentCatalog = [
  { ref: "z/shared", provider: "z", displayName: "First connection", model: "shared", current: false },
  { ref: "a/shared", provider: "a", displayName: "Second connection", model: "shared", current: true },
];
await act(async () => {
  Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "value")!.set!.call(catalogSearch, "");
  catalogSearch.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  window.dispatchEvent(new Event("reasonix:model-catalog-changed"));
  await new Promise(resolve => setTimeout(resolve, 0));
});

// Cold restoration can finish after the initial catalog read while retaining
// the same tab and model label. Readiness and session identity must refresh
// the connection without requiring the user to open the menu.
await act(async () => {
  (document.querySelector(".modelsw__trigger") as HTMLButtonElement).click();
  currentCatalog = currentCatalog.map(m => ({ ...m, current: m.provider === "z" }));
  root.render(renderSwitcher("shared", "tab-restored", false, "session-one"));
});
await act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); });
const restoredLabel = () => document.querySelector(".modelsw__trigger")?.getAttribute("aria-label") ?? "";
if (!restoredLabel().includes("First connection")) throw new Error("cold catalog fixture did not load");
await act(async () => {
  currentCatalog = currentCatalog.map(m => ({ ...m, current: m.provider === "a" }));
  root.render(renderSwitcher("shared", "tab-restored", true, "session-one"));
});
await act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); });
if (!restoredLabel().includes("Second connection")) throw new Error("ready session retained the startup connection label");
await act(async () => {
  currentCatalog = currentCatalog.map(m => ({ ...m, current: m.provider === "z" }));
  root.render(renderSwitcher("shared", "tab-restored", true, "session-two"));
});
await act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); });
if (!restoredLabel().includes("First connection")) throw new Error("resumed session retained another session's connection label");

await act(async () => {
  (document.querySelector(".modelsw__trigger") as HTMLButtonElement).click();
  await new Promise(resolve => setTimeout(resolve, 0));
});
if (!document.querySelector(".modelsw__menu")) throw new Error("model menu did not open before suspension");
await act(async () => { root.render(renderSwitcher("shared", "tab-restored", true, "session-two", true, 1)); });
await act(async () => { root.render(renderSwitcher("shared", "tab-restored", true, "session-two", false, 1)); });
if (document.querySelector(".modelsw__menu")) throw new Error("suspended model menu reopened after recovery");

await act(async () => root.unmount());
console.log("model switcher refresh: PASS");
