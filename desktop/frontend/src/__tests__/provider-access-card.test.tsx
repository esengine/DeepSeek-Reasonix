// Run: tsx src/__tests__/provider-access-card.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import {
  AddProviderPanel,
  ProviderAccessCard,
  providerAccessGroups,
  SettingsPanel,
  type ProviderAccessGroup,
} from "../components/SettingsPanel";
import { LocaleProvider } from "../lib/i18n";
import type { AppBindings } from "../lib/bridge";
import type { ProviderPresetView, ProviderView, SettingsView } from "../lib/types";
import { baseSettings } from "../test-support/settingsTestFixtures";

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

function customGroup(provider: ProviderView): ProviderAccessGroup {
  return {
    ...group([provider]),
    id: `custom:${provider.name}`,
    label: provider.name,
    builtIn: false,
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

let removedCustomProviders: string[] = [];
await act(async () => {
  root.render(renderCard(customGroup({ ...deepSeekAnthropic, name: "my-proxy", builtIn: false }), {
    onDelete: async (providers) => {
      removedCustomProviders = providers.map((provider) => provider.name);
    },
  }));
  await flushPromises();
});
const defaultCustomMoreButton = rootEl.querySelector<HTMLButtonElement>('button[aria-haspopup="menu"]');
ok(defaultCustomMoreButton?.disabled === false, "configured custom providers keep the removal menu available");
await act(async () => {
  defaultCustomMoreButton?.click();
  await flushPromises();
});
let deleteDefaultCustomButton = Array.from(document.querySelectorAll("button"))
  .find((button) => button.textContent?.trim() === "Remove access") as HTMLButtonElement | undefined;
await act(async () => {
  deleteDefaultCustomButton?.click();
  await flushPromises();
});
deleteDefaultCustomButton = Array.from(document.querySelectorAll("button"))
  .find((button) => button.textContent?.trim() === "Confirm delete provider") as HTMLButtonElement | undefined;
ok(deleteDefaultCustomButton !== undefined, "configured custom provider can be confirmed for deletion");
await act(async () => {
  deleteDefaultCustomButton?.click();
  await flushPromises();
});
ok(
  removedCustomProviders.join(",") === "my-proxy",
  "configured custom provider removal submits the selected provider",
);

const defaultCustomSettings: SettingsView = baseSettings("standard");
const defaultCustomProvider: ProviderView = {
  ...deepSeekAnthropic,
  name: "my-proxy",
  builtIn: false,
  added: true,
  kind: "openai",
  baseUrl: "https://proxy.example/v1",
  models: ["my-model"],
  default: "my-model",
  apiKeyEnv: "",
  keySet: true,
  configured: true,
  webSearch: false,
  serverWebSearchCapability: false,
};
defaultCustomSettings.defaultModel = "my-proxy/my-model";
defaultCustomSettings.providers = [defaultCustomProvider];
defaultCustomSettings.providerKinds = ["openai"];
let removedDefaultCustomProviders: string[] = [];
window.go = {
  main: {
    App: {
      Settings: async () => defaultCustomSettings,
      RemoveProviderAccesses: async (names: string[]) => {
        removedDefaultCustomProviders = [...names];
        defaultCustomSettings.providers = defaultCustomSettings.providers.filter((provider) => !names.includes(provider.name));
      },
    } as Partial<AppBindings> as AppBindings,
  },
};
const settingsRootEl = document.createElement("div");
document.body.appendChild(settingsRootEl);
const settingsRoot = createRoot(settingsRootEl);
await act(async () => {
  settingsRoot.render(
    <LocaleProvider>
      <SettingsPanel
        initialTab="models"
        initialFocus={{ target: "model-access", requestId: 1 }}
        desktopPlatform="linux"
        onClose={() => undefined}
        onChanged={() => undefined}
        onUseSubagent={() => undefined}
      />
    </LocaleProvider>,
  );
  await flushPromises();
  await flushPromises();
});
const defaultCustomSettingsMoreButton = settingsRootEl.querySelector<HTMLButtonElement>('button[aria-haspopup="menu"]');
ok(
  defaultCustomSettingsMoreButton?.disabled === false,
  "current default custom provider keeps the removal menu available",
);
await act(async () => {
  defaultCustomSettingsMoreButton?.click();
  await flushPromises();
});
let deleteCurrentDefaultButton = Array.from(document.querySelectorAll("button"))
  .find((button) => button.textContent?.trim() === "Remove access") as HTMLButtonElement | undefined;
await act(async () => {
  deleteCurrentDefaultButton?.click();
  await flushPromises();
});
deleteCurrentDefaultButton = Array.from(document.querySelectorAll("button"))
  .find((button) => button.textContent?.trim() === "Confirm delete provider") as HTMLButtonElement | undefined;
ok(deleteCurrentDefaultButton !== undefined, "current default custom provider can be confirmed for deletion");
await act(async () => {
  deleteCurrentDefaultButton?.click();
  await flushPromises();
  await flushPromises();
});
ok(
  removedDefaultCustomProviders.join(",") === "my-proxy",
  "current default custom provider removal reaches the settings backend",
);
await act(async () => {
  settingsRoot.unmount();
});
settingsRootEl.remove();

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
        key="official-installed"
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

const openCodePreset = (
  id: string,
  label: string,
  providerNames: string[],
  displayTier: "primary" | "advanced" | "compatibility",
  displaySection: "go" | "zen" = "go",
): ProviderPresetView => ({
  id,
  label,
  description: `${label} description`,
  keyEnv: displaySection === "zen" ? "OPENCODE_API_KEY" : "OPENCODE_GO_API_KEY",
  recommended: id === "opencode-go-recommended",
  displayGroup: "opencode",
  displaySection,
  displayTier,
  routeKind: id === "opencode-go-recommended" ? "bundle" : "anthropic",
  providerNames,
  models: ["model"],
  added: false,
  status: "available",
  statusProviderNames: [],
  keySet: false,
  requiresKey: true,
  configured: false,
});

await act(async () => {
  root.render(
    <LocaleProvider>
      <AddProviderPanel
        key="opencode-quick-setup"
        mode="official"
        kinds={["anthropic", "openai", "responses"]}
        officialProviders={[{ ...deepSeekAnthropic, name: "deepseek" }]}
        providerPresets={[
          openCodePreset("opencode-go-recommended", "OpenCode Go (Recommended)", ["opencode-go", "opencode-go-anthropic", "opencode-go-responses"], "primary"),
          openCodePreset("opencode-go-anthropic", "OpenCode Go Anthropic", ["opencode-go-anthropic"], "advanced"),
          openCodePreset("opencode-go-responses", "OpenCode Go Responses", ["opencode-go-responses"], "advanced"),
          openCodePreset("opencode-go", "OpenCode Go Chat", ["opencode-go"], "compatibility"),
          openCodePreset("opencode-zen-anthropic", "OpenCode Zen", ["opencode-zen-anthropic"], "primary", "zen"),
        ]}
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
ok(rootEl.textContent?.includes("OpenCode Go Anthropic") === false, "OpenCode setup hides protocol-specific provider entries");
ok(rootEl.textContent?.includes("Advanced routes") === false, "OpenCode setup does not expose an advanced-route decision");
ok(rootEl.textContent?.includes("Compatibility") === false, "OpenCode setup hides compatibility entries from new users");
const featuredProviders = rootEl.querySelector(".provider-featured-grid");
ok(
  featuredProviders?.textContent?.includes("DeepSeek") === true
    && featuredProviders.textContent.includes("OpenCode Go"),
  "OpenCode Go and DeepSeek are presented as same-level recommended providers",
);
ok(
  featuredProviders?.querySelectorAll(".provider-template-card").length === 2
    && featuredProviders.textContent?.includes("OpenCode Zen") === false,
  "the top-level provider row contains only DeepSeek and OpenCode Go",
);
ok(
  rootEl.querySelector<HTMLInputElement>('input[placeholder*="OPENCODE_GO_API_KEY"]') !== null,
  "OpenCode Go opens directly on its API key field",
);
ok(
  Array.from(rootEl.querySelectorAll("button")).some((button) => button.textContent?.trim() === "Connect and start using"),
  "OpenCode Go setup exposes one outcome-oriented primary action",
);
ok(rootEl.querySelector('[role="tablist"]') === null, "OpenCode Go quick setup does not ask users to choose a configuration mode");
const chooseAnotherProvider = Array.from(rootEl.querySelectorAll("button"))
  .find((button) => button.textContent?.trim() === "Choose another provider") as HTMLButtonElement | undefined;
await act(async () => {
  chooseAnotherProvider?.click();
  await flushPromises();
});
ok(rootEl.textContent?.includes("OpenCode Zen") === true, "OpenCode Zen remains an independent product choice");

const openCodeProviders: ProviderView[] = [
  { ...deepSeekAnthropic, builtIn: false, name: "opencode-go", kind: "openai", baseUrl: "https://opencode.ai/zen/go/v1", apiKeyEnv: "OPENCODE_GO_API_KEY", webSearch: false },
  { ...deepSeekAnthropic, builtIn: false, name: "opencode-go-anthropic", baseUrl: "https://opencode.ai/zen/go", apiKeyEnv: "OPENCODE_GO_API_KEY", webSearch: false },
  { ...deepSeekAnthropic, builtIn: false, name: "opencode-go-responses", kind: "responses", baseUrl: "https://opencode.ai/zen/go/v1", apiKeyEnv: "OPENCODE_GO_API_KEY", webSearch: false },
];
const openCodeGroups = providerAccessGroups(openCodeProviders, ((key: string) => key) as never);
ok(openCodeGroups.length === 1, "installed OpenCode Go routes render as one product connection");
ok(openCodeGroups[0]?.id === "custom:opencode-go", "OpenCode Go connection has a stable product-level identity");

await act(async () => {
  root.render(renderCard(openCodeGroups[0]));
  await flushPromises();
});
const routeSettings = rootEl.querySelector<HTMLDetailsElement>("details.provider-route-settings");
ok(routeSettings !== null && routeSettings.open === false, "installed OpenCode Go keeps protocol routes collapsed by default");
ok(
  routeSettings?.querySelector("summary")?.textContent?.trim() === "Advanced route settings",
  "installed OpenCode Go exposes route controls only through an advanced disclosure",
);

let removedOpenCodeProviders: string[] = [];
await act(async () => {
  root.render(renderCard(openCodeGroups[0], {
    onDelete: async (providers) => {
      removedOpenCodeProviders = providers.map((provider) => provider.name);
    },
  }));
  await flushPromises();
});
const openCodeMoreButton = rootEl.querySelector<HTMLButtonElement>('button[aria-haspopup="menu"]');
await act(async () => {
  openCodeMoreButton?.click();
  await flushPromises();
});
let removeOpenCodeButton = Array.from(document.querySelectorAll("button"))
  .find((button) => button.textContent?.trim() === "Remove access") as HTMLButtonElement | undefined;
await act(async () => {
  removeOpenCodeButton?.click();
  await flushPromises();
});
removeOpenCodeButton = Array.from(document.querySelectorAll("button"))
  .find((button) => button.textContent?.trim() === "Confirm delete provider") as HTMLButtonElement | undefined;
await act(async () => {
  removeOpenCodeButton?.click();
  await flushPromises();
});
ok(
  removedOpenCodeProviders.join(",") === "opencode-go,opencode-go-anthropic,opencode-go-responses",
  "OpenCode Go group removal submits every installed route atomically",
);

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
