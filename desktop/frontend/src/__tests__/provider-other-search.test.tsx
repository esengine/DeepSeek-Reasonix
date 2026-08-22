// Run: tsx src/__tests__/provider-other-search.test.tsx
// The "Other providers" section in the add-provider panel filters its preset
// cards live as the user types. This suite covers default state, case-insensitive
// fuzzy matching, clearing, the empty state, and the add flow regression.

import { JSDOM } from "jsdom";

let passed = 0;
let failed = 0;

function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

console.log("\nother-provider search");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.CustomEvent = dom.window.CustomEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;
globalThis.sessionStorage = dom.window.sessionStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => undefined });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => undefined });
window.scrollTo = () => {};
window.matchMedia = () => ({
  matches: true,
  media: "",
  onchange: null,
  addListener: () => undefined,
  removeListener: () => undefined,
  addEventListener: () => undefined,
  removeEventListener: () => undefined,
  dispatchEvent: () => false,
});

// Import react-dom lazily so the attachEvent polyfill above is installed before
// react-dom snapshots its input-event support (same pattern as blank-project-dialog).
import React, { act } from "react";
const [{ createRoot }, { AddProviderPanel }, { LocaleProvider }] = await Promise.all([
  import("react-dom/client"),
  import("../components/SettingsPanel"),
  import("../lib/i18n"),
]);
const { ProviderPresetView } = await import("../lib/types").then((m) => m);

function makePreset(id: string, label: string, description: string, keyEnv: string): ProviderPresetView {
  return {
    id,
    label,
    description,
    keyEnv,
    recommended: false,
    displayGroup: "",
    displaySection: "",
    displayTier: "advanced",
    routeKind: "openai",
    providerNames: [id],
    models: ["model"],
    added: false,
    status: "available",
    statusProviderNames: [],
    keySet: false,
    requiresKey: true,
    configured: false,
  };
}

const presets = [
  makePreset("deepseek-anthropic", "DeepSeek Official Anthropic", "Separate official Anthropic-compatible entry.", "DEEPSEEK_API_KEY"),
  makePreset("longcat-openai", "LongCat OpenAI", "LongCat platform endpoint.", "LONGCAT_API_KEY"),
  makePreset("kimi-cn", "Kimi CN API", "Moonshot Chinese mainland endpoint.", "KIMI_API_KEY"),
];

const addedPresetIDs: string[] = [];

function panel() {
  return (
    <LocaleProvider>
      <AddProviderPanel
        mode="official"
        kinds={["anthropic", "openai"]}
        officialProviders={[]}
        providerPresets={presets}
        busy={false}
        onMode={() => undefined}
        onCancel={() => undefined}
        onAddOfficial={async () => undefined}
        onAddPreset={async (id) => {
          addedPresetIDs.push(id);
        }}
        onViewPresetConflict={() => undefined}
        onResetPreset={async () => undefined}
        onAddCustom={() => undefined}
      />
    </LocaleProvider>
  );
}

// The quick-setup screen shows by default; "Choose another provider" opens the
// full catalog that contains the Other providers section with its search box.
async function openFullCatalog(rootEl: HTMLElement) {
  const chooseAnother = Array.from(rootEl.querySelectorAll("button"))
    .find((button) => button.textContent?.trim() === "Choose another provider") as HTMLButtonElement | undefined;
  await act(async () => {
    chooseAnother?.click();
    await flushPromises();
  });
}

function setSearch(rootEl: HTMLElement, value: string) {
  const input = rootEl.querySelector<HTMLInputElement>(".provider-other-search");
  if (!input) throw new Error("search input missing");
  const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")?.set;
  setter?.call(input, value);
  input.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  input.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
}

function visibleCards(rootEl: HTMLElement): string[] {
  return Array.from(rootEl.querySelectorAll(".provider-template-grid .provider-template-card"))
    .map((card) => card.textContent ?? "");
}

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

// Case 1: default state — search box present, all providers listed, no filtering.
await act(async () => {
  root.render(panel());
  await flushPromises();
});
await openFullCatalog(rootEl);
const searchInput = rootEl.querySelector<HTMLInputElement>(".provider-other-search");
ok(searchInput !== null && searchInput.getAttribute("aria-label") === "Search model services...", "default state shows the search input next to the Other providers title");
ok(searchInput?.value === "", "default state starts with an empty search query");
const defaultCards = visibleCards(rootEl);
ok(defaultCards.length === 3, "default state lists every other provider");
ok(defaultCards.some((c) => c.includes("DeepSeek Official Anthropic")) && defaultCards.some((c) => c.includes("LongCat OpenAI")) && defaultCards.some((c) => c.includes("Kimi CN API")), "default state keeps all provider cards");
const headRow = rootEl.querySelector(".provider-preset-group__head");
ok(headRow?.querySelector("h4") !== null && headRow?.querySelector(".provider-other-search") !== null, "the search box sits beside the Other providers heading in the same row container");

// Case 2: typing filters live to matching providers only.
await act(async () => {
  setSearch(rootEl, "deep");
  await flushPromises();
});
const deepCards = visibleCards(rootEl);
ok(deepCards.length === 1 && deepCards[0]?.includes("DeepSeek Official Anthropic"), "typing \"deep\" keeps only the DeepSeek preset");

// Case 3: case-insensitive matching — all casings agree.
for (const query of ["deepseek", "DeepSeek", "DEEPSEEK"]) {
  await act(async () => {
    setSearch(rootEl, query);
    await flushPromises();
  });
  const cards = visibleCards(rootEl);
  ok(cards.length === 1 && cards[0]?.includes("DeepSeek Official Anthropic"), `case-insensitive search "${query}" keeps the DeepSeek preset`);
}

// Case 4: clearing the query restores every provider.
await act(async () => {
  setSearch(rootEl, "");
  await flushPromises();
});
ok(visibleCards(rootEl).length === 3, "clearing the query restores every other provider");
ok(rootEl.querySelector(".provider-preset-group__empty") === null, "clearing the query removes the empty state");

// Case 5: no matches shows the empty state without breaking the section.
await act(async () => {
  setSearch(rootEl, "this-model-does-not-exist");
  await flushPromises();
});
const emptyState = rootEl.querySelector<HTMLElement>(".provider-preset-group__empty");
ok(emptyState !== null && emptyState.textContent === "No matching model services", "an unmatched query shows the empty state");
ok(rootEl.querySelector(".provider-preset-group__head") !== null, "the heading and search box remain visible in the empty state");
ok(rootEl.querySelector(".provider-template-grid") === null, "the empty state removes the card grid");

// Case 6: regression — selecting a card and confirming still runs the original add flow.
await act(async () => {
  setSearch(rootEl, "");
  await flushPromises();
});
const kimiCard = Array.from(rootEl.querySelectorAll(".provider-template-grid .provider-template-card"))
  .find((card) => card.textContent?.includes("Kimi CN API")) as HTMLButtonElement | undefined;
ok(kimiCard !== undefined && !kimiCard.disabled, "provider cards stay clickable after filtering is cleared");
await act(async () => {
  kimiCard?.click();
  await flushPromises();
});
const addButton = Array.from(rootEl.querySelectorAll(".prov-card__actions button"))
  .find((button) => button.textContent?.trim() === "Add provider") as HTMLButtonElement | undefined;
ok(addButton !== undefined && !addButton.disabled, "the confirm action enables after selecting a matching provider card");
await act(async () => {
  addButton?.click();
  await flushPromises();
});
ok(addedPresetIDs.includes("kimi-cn"), "clicking a provider card still triggers the preset add flow");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
