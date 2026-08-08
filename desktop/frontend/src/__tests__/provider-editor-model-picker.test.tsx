// Run: tsx src/__tests__/provider-editor-model-picker.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { LocaleProvider } from "../lib/i18n";
import { ProviderEditor, ProviderEditorModelPicker, providerSupportsServerWebSearch } from "../components/SettingsPanel";
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

function renderPicker(candidates: string[], visionCapability: "configurable" | "unsupported" = "configurable") {
  return (
    <LocaleProvider>
      <ProviderEditorModelPicker
        candidates={candidates}
        selectedModels={[]}
        visionModels={[]}
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
ok(providerSupportsServerWebSearch("responses", "https://api.deepseek.com"), "DeepSeek Responses exposes server-side web search");
ok(providerSupportsServerWebSearch("anthropic", "https://api.deepseek.com/anthropic"), "DeepSeek Anthropic exposes server-side web search");
ok(!providerSupportsServerWebSearch("openai", "https://api.deepseek.com"), "DeepSeek Chat Completions does not expose server-side web search");
ok(!providerSupportsServerWebSearch("responses", "https://relay.deepseek.com"), "DeepSeek-like subdomains do not inherit official defaults");
ok(!providerSupportsServerWebSearch("responses", "https://api.deepseek.com/anthropic"), "Responses rejects the Anthropic base path");
ok(!providerSupportsServerWebSearch("anthropic", "https://api.deepseek.com"), "Anthropic requires its documented base path");
ok(!providerSupportsServerWebSearch("responses", "http://api.deepseek.com"), "insecure DeepSeek URLs do not inherit official defaults");

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
};

function renderProviderEditor(initial?: ProviderView) {
  return (
    <LocaleProvider>
      <ProviderEditor
        initial={initial}
        kinds={["openai"]}
        busy={false}
        onCancel={() => undefined}
        onSave={() => undefined}
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
ok(rootEl.textContent?.includes("No image input") === true, "DeepSeek Responses editor labels image input as unsupported");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
