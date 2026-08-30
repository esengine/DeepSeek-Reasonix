// Run: tsx src/__tests__/provider-name-readonly.test.tsx

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { ProviderEditor } from "../components/SettingsPanel";
import { LocaleProvider } from "../lib/i18n";
import type { ProviderView } from "../lib/types";
import { en } from "../locales/en";
import { zh } from "../locales/zh";
import { zhTW } from "../locales/zh-TW";

const here = dirname(fileURLToPath(import.meta.url));
const styles = readFileSync(resolve(here, "../styles.css"), "utf8");

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

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function matchingBlocks(selector: string): string[] {
  const blocks: string[] = [];
  const rule = /([^{}]+)\{([^{}]*)\}/g;
  let match: RegExpExecArray | null;
  while ((match = rule.exec(styles)) !== null) {
    const selectors = match[1].split(",").map((part) => part.trim());
    if (selectors.includes(selector)) blocks.push(match[2]);
  }
  return blocks;
}

function finalDeclaration(selector: string, property: string): string | undefined {
  let value: string | undefined;
  for (const block of matchingBlocks(selector)) {
    const declaration = new RegExp(`(?:^|;)\\s*${property}\\s*:\\s*([^;]+)`, "g");
    let match: RegExpExecArray | null;
    while ((match = declaration.exec(block)) !== null) {
      value = match[1].trim();
    }
  }
  return value;
}

const customProvider: ProviderView = {
  name: "my-proxy",
  builtIn: false,
  added: true,
  kind: "openai",
  baseUrl: "https://example.com/v1",
  models: ["demo"],
  visionModels: [],
  visionModelsConfigured: false,
  modelsUrl: "",
  default: "demo",
  apiKeyEnv: "",
  keySet: false,
  balanceUrl: "",
  contextWindow: 128_000,
  reasoningProtocol: "",
  thinking: "",
  supportedEfforts: [],
  defaultEffort: "",
};

function nameHint(root: Element): Element | null {
  const input = root.querySelector<HTMLInputElement>('input[placeholder="e.g. my-proxy"]');
  const next = input?.nextElementSibling;
  return next?.classList.contains("mem-hint") ? next : null;
}

function renderEditor(initial?: ProviderView) {
  return (
    <LocaleProvider>
      <ProviderEditor
        key={initial?.name ?? "new-provider"}
        initial={initial}
        kinds={["openai"]}
        busy={false}
        onCancel={() => undefined}
        onSave={() => undefined}
      />
    </LocaleProvider>
  );
}

console.log("\nprovider name readonly");

eq(en["settings.customProviderNameReadonlyHint"], "Changing the provider name is not supported yet", "English rename hint");
eq(zh["settings.customProviderNameReadonlyHint"], "暂不支持供应商名称修改", "Simplified Chinese rename hint");
eq(zhTW["settings.customProviderNameReadonlyHint"], "暫不支援供應商名稱修改", "Traditional Chinese rename hint");

eq(finalDeclaration(".provider-name-input:disabled", "opacity"), "0.6", "disabled provider-name input is faded");
eq(finalDeclaration(".provider-name-input:disabled", "cursor"), "not-allowed", "disabled provider-name input uses not-allowed cursor");
eq(finalDeclaration(".mem-hint.provider-name-readonly-hint", "color"), "var(--fg-dim)", "readonly hint uses the stronger secondary text color");
eq(finalDeclaration(".mem-input:disabled", "opacity"), undefined, "no global mem-input:disabled fade rule remains");
eq(finalDeclaration(".mem-select:disabled", "opacity"), undefined, "no global mem-select:disabled fade rule remains");

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

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(renderEditor());
  await flushPromises();
});
const newNameInput = rootEl.querySelector<HTMLInputElement>('input[placeholder="e.g. my-proxy"]');
ok(newNameInput?.disabled !== true, "new custom provider name stays editable");
ok(newNameInput?.classList.contains("mem-input") === true, "provider name keeps mem-input base styling");
ok(newNameInput?.classList.contains("provider-name-input") === true, "provider name carries the scoped provider-name-input class");
ok(Boolean(newNameInput?.id), "new custom provider name has a stable input id");
eq(rootEl.querySelector<HTMLLabelElement>(`label[for="${newNameInput?.id}"]`)?.textContent, en["settings.customProviderName"], "new custom provider name has a programmatic label");
eq(newNameInput?.getAttribute("aria-describedby"), null, "editable provider name omits the readonly description reference");
ok(nameHint(rootEl) === null, "new custom provider editor omits the rename hint");

await act(async () => {
  root.render(renderEditor(customProvider));
  await flushPromises();
});
const existingNameInput = rootEl.querySelector<HTMLInputElement>('input[placeholder="e.g. my-proxy"]');
ok(existingNameInput?.disabled === true, "existing custom provider name is locked");
ok(existingNameInput?.classList.contains("mem-input") === true, "locked provider name keeps mem-input base styling");
ok(existingNameInput?.classList.contains("provider-name-input") === true, "locked provider name carries the scoped provider-name-input class");
eq(rootEl.querySelector<HTMLLabelElement>(`label[for="${existingNameInput?.id}"]`)?.textContent, en["settings.customProviderName"], "locked provider name has a programmatic label");
const existingNameHint = nameHint(rootEl);
eq(existingNameHint?.textContent, en["settings.customProviderNameReadonlyHint"], "existing custom provider editor shows the rename hint");
ok(existingNameHint?.classList.contains("provider-name-readonly-hint") === true, "rename hint carries the stronger contrast class");
ok(Boolean(existingNameHint?.id), "rename hint has a stable id");
eq(existingNameInput?.getAttribute("aria-describedby"), existingNameHint?.id, "locked provider name references the rename hint");

await act(async () => {
  root.unmount();
});
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
