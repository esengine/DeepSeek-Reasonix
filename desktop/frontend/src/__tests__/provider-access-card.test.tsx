// Run: tsx src/__tests__/provider-access-card.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import {
  AddProviderPanel,
  ProviderAccessCard,
  type ProviderAccessGroup,
} from "../components/SettingsPanel";
import { LocaleProvider } from "../lib/i18n";
import type { ProviderView } from "../lib/types";

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

const deepSeekAnthropic: ProviderView = {
  name: "deepseek-flash",
  builtIn: true,
  added: true,
  kind: "anthropic",
  baseUrl: "https://api.deepseek.com/anthropic",
  models: ["deepseek-v4-flash"],
  visionModels: [],
  visionModelsConfigured: true,
  visionCapability: "unsupported",
  modelsUrl: "",
  default: "deepseek-v4-flash",
  apiKeyEnv: "DEEPSEEK_API_KEY",
  keySet: true,
  configured: true,
  balanceUrl: "",
  contextWindow: 128_000,
  reasoningProtocol: "deepseek",
  thinking: "",
  webSearch: true,
  serverWebSearchCapability: true,
  supportedEfforts: [],
  defaultEffort: "",
};

const deepSeekLegacyOpenAI: ProviderView = {
  ...deepSeekAnthropic,
  name: "deepseek-pro",
  kind: "openai",
  baseUrl: "https://api.deepseek.com",
  models: ["deepseek-v4-pro"],
  default: "deepseek-v4-pro",
  webSearch: false,
  serverWebSearchCapability: false,
};

function group(providers: ProviderView[]): ProviderAccessGroup {
  return {
    id: "builtin:deepseek",
    label: "DeepSeek Official",
    description: "",
    builtIn: true,
    providers,
    apiKeyEnv: "DEEPSEEK_API_KEY",
    keySet: true,
    requiresKey: true,
    configured: true,
    baseUrl: providers[0]?.baseUrl ?? "",
    kind: providers[0]?.kind ?? "",
    models: providers.flatMap((provider) => provider.models),
    recommendedUpgradeAvailable: false,
  };
}

function renderCard(
  providerGroup: ProviderAccessGroup,
  actions: {
    onRefresh?: (provider: ProviderView) => void;
    onDelete?: (providers: ProviderView[]) => Promise<void>;
  } = {},
) {
  return (
    <LocaleProvider>
      <ProviderAccessCard
        group={providerGroup}
        busy={false}
        fetching={false}
        defaultProvider=""
        editing={null}
        kinds={["anthropic", "openai"]}
        onEdit={() => undefined}
        onCancelEdit={() => undefined}
        onSave={() => undefined}
        onRefresh={actions.onRefresh ?? (() => undefined)}
        onToggleDraftModel={() => undefined}
        onToggleDraftVision={() => undefined}
        onSelectAllDraftModels={() => undefined}
        onClearDraftModels={() => undefined}
        onCancelDraftModels={() => undefined}
        onSaveDraftModels={() => undefined}
        onToggleWebSearch={() => undefined}
        onUpgradeRecommended={() => undefined}
        onSaveEditorKey={async () => undefined}
        onDelete={actions.onDelete}
      />
    </LocaleProvider>
  );
}

console.log("\nprovider access card");

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(renderCard(group([deepSeekAnthropic, deepSeekLegacyOpenAI])));
  await flushPromises();
});
ok(
  rootEl.querySelector<HTMLInputElement>('input[role="switch"]') === null,
  "mixed supported and unsupported profiles hide the grouped server web-search switch",
);
ok(
  rootEl.textContent?.includes("deepseek-v4-flash") === true
    && rootEl.textContent?.includes("deepseek-v4-pro") === true,
  "mixed-profile cards keep the complete model summary visible",
);

let refreshedProvider = "";
await act(async () => {
  root.render(renderCard(group([deepSeekAnthropic, deepSeekLegacyOpenAI]), {
    onRefresh: (provider) => {
      refreshedProvider = provider.name;
    },
  }));
  await flushPromises();
});
const refreshButtons = Array.from(rootEl.querySelectorAll("button"))
  .filter((button) => button.textContent?.trim() === "Refresh models") as HTMLButtonElement[];
ok(refreshButtons.length === 2, "multi-profile cards expose one model refresh action per profile");
await act(async () => {
  refreshButtons[1]?.click();
  await flushPromises();
});
ok(refreshedProvider === "deepseek-pro", "profile refresh targets the selected provider instead of the first profile");

let removedProviders: string[] = [];
await act(async () => {
  root.render(renderCard(group([deepSeekAnthropic, deepSeekLegacyOpenAI]), {
    onDelete: async (providers) => {
      removedProviders = providers.map((provider) => provider.name);
    },
  }));
  await flushPromises();
});
const moreButton = rootEl.querySelector<HTMLButtonElement>('button[aria-haspopup="menu"]');
await act(async () => {
  moreButton?.click();
  await flushPromises();
});
let removeButton = Array.from(document.querySelectorAll("button"))
  .find((button) => button.textContent?.trim() === "Remove access") as HTMLButtonElement | undefined;
await act(async () => {
  removeButton?.click();
  await flushPromises();
});
removeButton = Array.from(document.querySelectorAll("button"))
  .find((button) => button.textContent?.trim() === "Confirm remove access") as HTMLButtonElement | undefined;
ok(removeButton !== undefined, "grouped provider removal requires inline confirmation");
await act(async () => {
  removeButton?.click();
  await flushPromises();
});
ok(
  removedProviders.join(",") === "deepseek-flash,deepseek-pro",
  "card-level removal submits every grouped provider in one action",
);

await act(async () => {
  root.render(renderCard(group([
    deepSeekAnthropic,
    { ...deepSeekAnthropic, name: "deepseek-pro", models: ["deepseek-v4-pro"], default: "deepseek-v4-pro", webSearch: false },
  ])));
  await flushPromises();
});
const mixedStateSwitch = rootEl.querySelector<HTMLInputElement>('input[role="switch"]');
ok(mixedStateSwitch !== null, "all supported grouped profiles expose server web search");
ok(mixedStateSwitch?.checked === false, "grouped switch is off until every profile has web search enabled");

await act(async () => {
  root.render(renderCard(group([
    deepSeekAnthropic,
    { ...deepSeekAnthropic, name: "deepseek-pro", models: ["deepseek-v4-pro"], default: "deepseek-v4-pro" },
  ])));
  await flushPromises();
});
ok(
  rootEl.querySelector<HTMLInputElement>('input[role="switch"]')?.checked === true,
  "grouped switch is on when every supported profile has web search enabled",
);

await act(async () => {
  root.render(
    <LocaleProvider>
      <AddProviderPanel
        mode="official"
        kinds={["anthropic", "openai"]}
        officialProviders={[{ ...deepSeekAnthropic, name: "deepseek" }]}
        providerPresets={[]}
        busy={false}
        onMode={() => undefined}
        onCancel={() => undefined}
        onAddOfficial={async () => undefined}
        onAddPreset={async () => undefined}
        onViewPresetConflict={() => undefined}
        onResetPreset={async () => undefined}
        onAddCustom={() => undefined}
      />
    </LocaleProvider>,
  );
  await flushPromises();
});
const installedOfficialChoice = Array.from(rootEl.querySelectorAll("button"))
  .find((button) => button.textContent?.includes("DeepSeek"));
ok(installedOfficialChoice?.disabled === true, "installed official providers cannot be added again");
ok(installedOfficialChoice?.textContent?.includes("Added") === true, "installed official providers show their backend status");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
