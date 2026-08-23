// Run: tsx src/__tests__/provider-editor-model-picker.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { LocaleProvider } from "../lib/i18n";
import {
  ProviderEditor,
  ProviderEditorModelPicker,
  providerSupportsServerWebSearch,
  providerSupportsServerWebSearchForView,
  providerVisionCapabilityForView,
} from "../components/SettingsPanel";
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
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;
globalThis.sessionStorage = dom.window.sessionStorage;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
window.scrollTo = () => {};

function renderPicker(
  candidates: string[],
  visionCapability: "configurable" | "unsupported" = "configurable",
  visionModels: string[] = [],
) {
  return (
    <LocaleProvider>
      <ProviderEditorModelPicker
        candidates={candidates}
        selectedModels={[]}
        visionModels={visionModels}
        visionCapability={visionCapability}
        contextWindows={{}}
        disabled={false}
        onToggleModel={() => undefined}
        onToggleVision={() => undefined}
        onContextWindowChange={() => undefined}
        onSelectAll={() => undefined}
        onClear={() => undefined}
      />
    </LocaleProvider>
  );
}

console.log("\nprovider editor model picker");

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

let threw = false;
try {
  await act(async () => {
    root.render(renderPicker([]));
    await flushPromises();
  });
  await act(async () => {
    root.render(renderPicker(["zen-v1", "zen-v1-pro"]));
    await flushPromises();
  });
} catch (error) {
  threw = true;
  process.stdout.write(`  ERROR ${String(error)}\n`);
}

ok(!threw, "model picker can render after async model fetch returns candidates");
ok(rootEl.textContent?.includes("zen-v1") === true, "model picker shows fetched custom provider models");

await act(async () => {
  root.render(renderPicker(["deepseek-v4-flash"], "unsupported"));
  await flushPromises();
});
ok(rootEl.textContent?.includes("No image input") === true, "known text-only DeepSeek models show a read-only image capability");
ok(rootEl.querySelectorAll('input[type="checkbox"]').length === 1, "text-only model card does not render a second image checkbox");
await act(async () => {
  root.render(renderPicker(["deepseek-v4-flash-vision-exp"], "unsupported", ["deepseek-v4-flash-vision-exp"]));
  await flushPromises();
});
ok(rootEl.textContent?.includes("No image input") !== true, "pinned DeepSeek vision SKU does not show the text-only image label");
ok(rootEl.querySelectorAll('input[type="checkbox"]').length === 1, "pinned DeepSeek vision SKU does not render a configurable image checkbox");
await act(async () => {
  root.render(renderPicker(
    ["deepseek-v4-flash", "deepseek-v4-flash-vision-exp"],
    "configurable",
    ["deepseek-v4-flash-vision-exp"],
  ));
  await flushPromises();
});
ok(rootEl.querySelectorAll('input[type="checkbox"]').length === 4, "configurable DeepSeek models expose image-input checkboxes");
ok(rootEl.textContent?.includes("No image input") !== true, "configurable DeepSeek models do not use the read-only image-unsupported label");
ok(providerSupportsServerWebSearch("responses", "https://api.deepseek.com"), "DeepSeek Responses exposes server-side web search");
ok(providerSupportsServerWebSearch("anthropic", "https://api.deepseek.com/anthropic"), "DeepSeek Anthropic exposes server-side web search");
ok(!providerSupportsServerWebSearch("openai", "https://api.deepseek.com"), "DeepSeek Chat Completions does not expose server-side web search");
ok(!providerSupportsServerWebSearch("responses", "https://relay.deepseek.com"), "DeepSeek-like subdomains do not inherit official defaults");
ok(!providerSupportsServerWebSearch("responses", "https://api.deepseek.com/anthropic"), "Responses rejects the Anthropic base path");
ok(!providerSupportsServerWebSearch("anthropic", "https://api.deepseek.com"), "Anthropic requires its documented base path");
ok(!providerSupportsServerWebSearch("responses", "http://api.deepseek.com"), "insecure DeepSeek URLs do not inherit official defaults");
ok(
  providerSupportsServerWebSearchForView({
    kind: "anthropic",
    baseUrl: "https://gateway.example/anthropic",
    serverWebSearchCapability: true,
  }),
  "backend verification can enable server-side web search for future curated endpoints",
);
ok(
  !providerSupportsServerWebSearchForView({
    kind: "anthropic",
    baseUrl: "https://api.deepseek.com/anthropic",
    serverWebSearchCapability: false,
  }),
  "backend capability overrides endpoint inference when explicitly unsupported",
);
ok(
  providerSupportsServerWebSearchForView({ kind: "anthropic", baseUrl: "https://api.deepseek.com/anthropic" }),
  "older backend payloads retain the exact DeepSeek endpoint fallback",
);
ok(
  !providerSupportsServerWebSearchForView({ kind: "anthropic", baseUrl: "https://gateway.example/anthropic" }),
  "older backend payloads do not infer support for arbitrary compatible endpoints",
);
ok(
  providerVisionCapabilityForView({ kind: "openai", baseUrl: "https://custom.example/v1", visionCapability: "unsupported" }) === "unsupported",
  "backend vision capability overrides frontend endpoint inference",
);
ok(
  providerVisionCapabilityForView({ kind: "openai", baseUrl: "https://api.deepseek.com" }) === "configurable",
  "official DeepSeek Settings uses the same image-input checkboxes as other providers",
);
ok(
  providerVisionCapabilityForView({ kind: "anthropic", baseUrl: "https://eu.deepseek.com/anthropic" }) === "configurable",
  "regional DeepSeek Settings also expose per-model image-input checkboxes",
);
ok(
  providerVisionCapabilityForView({ kind: "responses", baseUrl: "https://deepseek.com" }) === "configurable",
  "the DeepSeek apex does not inherit official vision restrictions",
);
ok(
  providerVisionCapabilityForView({ kind: "openai", baseUrl: "https://deepseek.com.example/v1" }) === "configurable",
  "lookalike domains with a DeepSeek prefix do not inherit official vision restrictions",
);
ok(
  providerVisionCapabilityForView({ kind: "openai", baseUrl: "https://api.deepseek.com.example/v1" }) === "configurable",
  "lookalike domains with an official-host prefix remain configurable",
);

const builtInProvider: ProviderView = {
  name: "deepseek",
  builtIn: true,
  added: true,
  kind: "openai",
  baseUrl: "https://api.deepseek.com",
  models: ["deepseek-chat"],
  visionModels: [],
  visionModelsConfigured: false,
  modelsUrl: "",
  default: "deepseek-chat",
  apiKeyEnv: "",
  keySet: false,
  balanceUrl: "",
  contextWindow: 128_000,
  reasoningProtocol: "deepseek",
  thinking: "",
  supportedEfforts: [],
  defaultEffort: "",
};

const deepSeekResponsesProvider: ProviderView = {
  ...builtInProvider,
  name: "deepseek-responses",
  builtIn: false,
  kind: "responses",
  models: ["deepseek-v4-flash"],
  default: "deepseek-v4-flash",
  webSearch: true,
  serverWebSearchCapability: true,
};

const longCatAnthropicProvider: ProviderView = {
  ...builtInProvider,
  name: "longcat-anthropic",
  builtIn: false,
  kind: "anthropic",
  baseUrl: "https://api.longcat.chat/anthropic",
  models: ["LongCat-2.0"],
  default: "LongCat-2.0",
  serverWebSearchCapability: false,
};

const backendUnsupportedCustomProvider: ProviderView = {
  ...builtInProvider,
  name: "deepseek-regional",
  builtIn: false,
  baseUrl: "https://eu.deepseek.com/v1",
  models: ["deepseek-v4-pro"],
  default: "deepseek-v4-pro",
  visionCapability: "unsupported",
};

const legacyChatURLProvider: ProviderView = {
  ...backendUnsupportedCustomProvider,
  name: "legacy-chat-url",
  chatUrl: "https://legacy.example.com/chat/completions/",
};

function renderProviderEditor(initial?: ProviderView, onSave: (provider: ProviderView) => void | Promise<void> = () => undefined) {
  return (
    <LocaleProvider>
      <ProviderEditor
        key={initial?.name ?? "new-provider"}
        initial={initial}
        kinds={["openai"]}
        busy={false}
        onCancel={() => undefined}
        onSave={onSave}
      />
    </LocaleProvider>
  );
}

let editorThrew = false;
try {
  await act(async () => {
    root.render(renderProviderEditor(builtInProvider));
    await flushPromises();
  });
  await act(async () => {
    root.render(renderProviderEditor());
    await flushPromises();
  });
} catch (error) {
  editorThrew = true;
  process.stdout.write(`  ERROR ${String(error)}\n`);
}

ok(!editorThrew, "provider editor can switch from built-in to custom without changing hook order");
ok(rootEl.textContent?.includes("OpenAI-compatible") === true, "provider editor renders the custom provider fields after the switch");
ok(rootEl.textContent?.includes("Kimi K3 reasoning (low / high / max)") === true, "custom provider editor exposes the explicit Kimi K3 reasoning protocol");
const providerUrlInput = rootEl.querySelector<HTMLInputElement>(".provider-url-input");
ok(rootEl.querySelectorAll('input[type="radio"]').length === 0, "custom provider editor exposes only one API address input");
ok(providerUrlInput?.value === "", "new custom providers start with an empty exact request address");
const providerUrlLabel = Array.from(rootEl.querySelectorAll<HTMLLabelElement>("label")).find(
  (label) => label.htmlFor === providerUrlInput?.id,
);
ok(Boolean(providerUrlLabel) && providerUrlInput?.getAttribute("aria-describedby") !== null, "provider URL input has a programmatic label and description");
ok(rootEl.textContent?.includes("Reasonix uses it unchanged.") === true, "provider URL helper explains exact request behavior");
ok(rootEl.querySelector<HTMLInputElement>('input[placeholder="e.g. my-proxy"]')?.disabled !== true, "new custom provider name stays editable");
ok(rootEl.querySelector<HTMLInputElement>('input[placeholder="e.g. my-proxy"]')?.nextElementSibling?.classList.contains("mem-hint") !== true, "new custom provider editor omits the rename hint");

await act(async () => {
  root.render(<div />);
  await flushPromises();
});
await act(async () => {
  root.render(renderProviderEditor(deepSeekResponsesProvider));
  await flushPromises();
});
const webSearchSwitch = rootEl.querySelector<HTMLInputElement>('input[role="switch"]');
ok(rootEl.textContent?.includes("Server-side web search") === true, "DeepSeek Responses editor separates service capabilities from model selection");
ok(webSearchSwitch?.checked === true, "curated DeepSeek Responses capability is enabled in the editor");
ok(rootEl.textContent?.includes("Image input") === true, "DeepSeek Responses editor exposes per-model image-input checkboxes");
ok(rootEl.textContent?.includes("No image input") !== true, "DeepSeek Responses editor does not use the read-only image-unsupported label");

await act(async () => {
  root.render(renderProviderEditor(longCatAnthropicProvider));
  await flushPromises();
});
ok(rootEl.textContent?.includes("Server-side web search") === false, "unverified LongCat Anthropic endpoint hides server-side web search");
ok(rootEl.querySelector<HTMLInputElement>('input[role="switch"]') === null, "unverified LongCat endpoint exposes no recommended web-search switch");

await act(async () => {
  root.render(renderProviderEditor(backendUnsupportedCustomProvider));
  await flushPromises();
});
ok(
  rootEl.textContent?.includes("No image input") === true,
  "provider editor honors backend vision capability for endpoints outside the legacy frontend heuristic",
);
const customProviderUrlInput = rootEl.querySelector<HTMLInputElement>(".provider-url-input");
ok(rootEl.querySelectorAll('input[type="radio"]').length === 0, "existing custom providers no longer expose an address mode selector");
ok(customProviderUrlInput?.value === "https://eu.deepseek.com/v1/chat/completions", "legacy base-only providers display their previously effective request URL");
ok(rootEl.querySelector<HTMLInputElement>('input[placeholder="e.g. my-proxy"]')?.disabled === true, "existing custom provider name is locked");
ok(rootEl.querySelector<HTMLInputElement>('input[placeholder="e.g. my-proxy"]')?.nextElementSibling?.textContent === "Changing the provider name is not supported yet", "existing custom provider editor shows the rename hint");

let migratedProvider: ProviderView | undefined;
await act(async () => {
  root.render(renderProviderEditor(legacyChatURLProvider, (provider) => {
    migratedProvider = provider;
  }));
  await flushPromises();
});
const legacyProviderUrlInput = rootEl.querySelector<HTMLInputElement>(".provider-url-input");
ok(legacyProviderUrlInput?.value === "https://legacy.example.com/chat/completions", "legacy OpenAI chat URLs display their historically normalized effective endpoint");
const saveButton = Array.from(rootEl.querySelectorAll<HTMLButtonElement>("button")).find(
  (button) => button.textContent?.trim() === "Save",
);
await act(async () => {
  saveButton?.click();
  await flushPromises();
});
ok(migratedProvider?.requestUrl === "https://legacy.example.com/chat/completions" && migratedProvider?.baseUrl === legacyChatURLProvider.baseUrl, "saving migrates the legacy effective address without rewriting its baseUrl");
ok(migratedProvider?.chatUrl === migratedProvider?.requestUrl, "saving mirrors the normalized legacy OpenAI endpoint for previous releases");

let exactProvider: ProviderView | undefined;
await act(async () => {
  root.render(renderProviderEditor({
    ...legacyChatURLProvider,
    name: "exact-request-url",
    requestUrl: "https://exact.example.com/custom/?token=1",
  }, (provider) => {
    exactProvider = provider;
  }));
  await flushPromises();
});
const exactSaveButton = Array.from(rootEl.querySelectorAll<HTMLButtonElement>("button")).find(
  (button) => button.textContent?.trim() === "Save",
);
await act(async () => {
  exactSaveButton?.click();
  await flushPromises();
});
ok(exactProvider?.requestUrl === "https://exact.example.com/custom/?token=1" && exactProvider?.baseUrl === legacyChatURLProvider.baseUrl, "saving preserves an explicit requestUrl and independent baseUrl exactly");
ok(exactProvider?.chatUrl === exactProvider?.requestUrl, "saving mirrors the exact OpenAI request URL for previous releases");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
