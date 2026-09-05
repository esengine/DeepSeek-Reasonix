import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { applyModelDraft, modelDraftError } from "../lib/providerModelDraft";
import type { ProviderView, ProviderModelCapabilityView } from "../lib/types";

const dom = new JSDOM('<!doctype html><div id="root"></div>', { url: "http://localhost", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, HTMLElement: dom.window.HTMLElement, Node: dom.window.Node, localStorage: dom.window.localStorage, IS_REACT_ACT_ENVIRONMENT: true });
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
const { default: React, act } = await import("react");
const { createRoot } = await import("react-dom/client");
const { LocaleProvider } = await import("../lib/i18n");
const { ProviderModelsEditor } = await import("../components/ProviderModelsEditor");
const root = createRoot(document.getElementById("root")!);
const override = { model: "existing", contextWindow: 32768, maxOutputTokens: 4096, vision: null, reasoningProtocol: "kimi", supportedEfforts: ["high"], defaultEffort: "high" };
const auto = { model: "renamed", context: "", output: "-1", vision: "auto" as const };
const result = applyModelDraft([override], auto, "existing")[0];
assert.equal(result.reasoningProtocol, "kimi");
assert.deepEqual(result.supportedEfforts, ["high"]);
assert.equal(result.contextWindow, 0);
assert.equal(result.maxOutputTokens, -1);
assert.equal(result.vision, null);
assert.equal(modelDraftError({ ...auto, model: "existing" }, ["existing"]), "duplicate");
assert.equal(modelDraftError({ ...auto, context: "1.5" }, []), "context");
assert.equal(modelDraftError({ ...auto, output: "0" }, []), "output");

let provider = { name: "test", kind: "openai", baseUrl: "https://example.test/v1", models: ["existing"], modelOverrides: [override], modelCapabilities: [], visionModels: [], visionCapability: "configurable" } as unknown as ProviderView;
let changes = 0;
let resolveFetch: ((value: ProviderModelCapabilityView[]) => void) | undefined;
let rejectTest: ((reason: Error) => void) | undefined;
const render = () => root.render(<LocaleProvider><ProviderModelsEditor provider={provider} disabled={false} canFetch draft
  onFetch={() => new Promise((resolve) => { resolveFetch = resolve; })}
  onTest={() => new Promise((_, reject) => { rejectTest = reject; })}
  onChange={(models, modelOverrides, modelCapabilities) => { changes++; provider = { ...provider, models, modelOverrides, modelCapabilities }; render(); }}
/></LocaleProvider>);
const click = async (text: string) => {
  const button = Array.from(document.querySelectorAll("button")).find((b) => b.textContent?.trim() === text || b.getAttribute("aria-label") === text);
  assert.ok(button, `button ${text}`);
  await act(async () => { button.focus(); button.click(); });
};
const fill = async (input: HTMLInputElement, value: string) => {
  await act(async () => {
    Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")!.set!.call(input, value);
    input.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  });
};
await act(async () => render());
await click("Add manually");
assert.ok(document.querySelector('[role="dialog"]'));
assert.equal(document.activeElement?.tagName, "INPUT");
await click("Save to draft");
assert.match(document.querySelector('[role="alert"]')!.textContent!, /required/);
const modelID = document.querySelector<HTMLInputElement>('[role="dialog"] input')!;
await fill(modelID, "existing");
await click("Save to draft");
assert.match(document.querySelector('[role="alert"]')!.textContent!, /already added/);
await fill(modelID, "manual");
await click("Save to draft");
assert.deepEqual(provider.models, ["existing", "manual"]);
assert.equal(provider.modelOverrides?.find((m) => m.model === "manual")?.contextWindow, 0);

await click("Edit model existing");
let parentEscapes = 0;
const parentEscape = () => { parentEscapes++; };
document.addEventListener("keydown", parentEscape);
await act(async () => { document.activeElement?.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: "Escape", bubbles: true })); });
assert.equal(parentEscapes, 0);
assert.equal(document.querySelector('[role="dialog"]'), null);
assert.equal(document.activeElement?.getAttribute("aria-label"), "Edit model existing");
document.removeEventListener("keydown", parentEscape);

await click("Test model existing");
await click("Refresh models");
await act(async () => { rejectTest!(new Error("authentication failed")); });
assert.match(document.querySelector('[role="status"]')!.textContent!, /authentication failed/);
const catalog = ["existing", "remote"].map((model) => ({ model, inputModalities: ["text"], state: "unknown", source: "catalog" }));
await act(async () => { resolveFetch!(catalog); });
const checkboxes = document.querySelectorAll<HTMLInputElement>('[role="dialog"] input[type="checkbox"]');
assert.equal(checkboxes[0].disabled, true);
assert.equal(checkboxes[0].checked, true);
await act(async () => checkboxes[1].click());
await click("Add selected models");
assert.deepEqual(provider.models, ["existing", "manual", "remote"]);
assert.deepEqual(provider.modelOverrides?.[0], override);
assert.equal(changes, 2);

await click("Refresh models");
await act(async () => { provider = { ...provider, baseUrl: "https://other.test/v1" }; render(); });
await act(async () => { resolveFetch!(catalog); });
assert.equal(document.querySelector('[role="dialog"]'), null, "old endpoint discovery must not reopen a dialog");
await act(async () => root.unmount());
dom.window.close();
console.log("PASS provider model drafts, modal validation/focus/Escape, concurrent probes, add-only discovery, stale results");
