import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { ProviderEditor, ProviderEditorModelPicker } from "../components/SettingsPanel";
import { ModelImageInputControl } from "../components/ModelImageInputControl";
import { LocaleProvider } from "../lib/i18n";
import { imageInputHardBlocked, imageInputState, mergeImageInputModes } from "../lib/providerImageInput";
import type { AppBindings } from "../lib/bridge";
import type { ProviderModelCapabilityView, ProviderView } from "../lib/types";

const dom = new JSDOM('<div id="root"></div>', { url: "http://localhost/", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, Node: dom.window.Node, HTMLElement: dom.window.HTMLElement, Event: dom.window.Event, CustomEvent: dom.window.CustomEvent, MouseEvent: dom.window.MouseEvent, localStorage: dom.window.localStorage, sessionStorage: dom.window.sessionStorage, IS_REACT_ACT_ENVIRONMENT: true });
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
window.scrollTo = () => {};
const el = document.getElementById("root")!;
const root = createRoot(el);
const unknown: ProviderModelCapabilityView = { model: "relay-model", state: "unknown", source: "adapter", inputModalities: [], automaticState: "unknown", automaticSource: "adapter", imageInputEnableAllowed: true };
const initial: ProviderView = { name: "relay", kind: "openai", baseUrl: "https://relay.test/v1", requestUrl: "https://relay.test/v1/chat/completions", modelsUrl: "", models: ["relay-model"], visionModels: [], builtIn: false, added: true, default: "relay-model", apiKeyEnv: "TEST_KEY", keySet: true, balanceUrl: "", contextWindow: 0, reasoningProtocol: "", thinking: "", webSearch: false, supportedEfforts: [], defaultEffort: "", modelCapabilities: [unknown], modelOverrides: [{ model: "relay-model", vision: null, contextWindow: 123456, maxOutputTokens: 4321, reasoningProtocol: "openai", supportedEfforts: ["low"], defaultEffort: "low" }] };
const saved: ProviderView[] = [];
let cancelled = 0;
const render = async (node: React.ReactNode) => { await act(async () => { root.render(<LocaleProvider>{node}</LocaleProvider>); }); };
const editor = (key: string) => <ProviderEditor key={key} initial={initial} kinds={["openai"]} busy={false} onSave={(p) => { saved.push(p); }} onCancel={() => { cancelled++; }} />;
const modeInput = (mode: string) => el.querySelector<HTMLInputElement>(`[role="radiogroup"][aria-label="Image input mode for relay-model"] input[value="${mode}"]`)!;
const choose = async (mode: string) => { await act(async () => { const input = modeInput(mode); assert(input && !input.disabled); input.click(); }); };
const click = async (label: string) => { await act(async () => { const b = [...el.querySelectorAll("button")].find((b) => b.textContent?.trim() === label); assert(b, `missing button ${label}`); b.click(); }); };

await render(editor("first"));
assert(el.textContent?.includes("Image capability unknown"));
assert(el.querySelector('[role="radiogroup"]')?.classList.contains("set-seg"), "image policy must reuse the settings segmented control");
assert(modeInput("auto").closest("label")?.classList.contains("set-seg__btn--on"), "selected mode must use the shared settings selected state");
await choose("on");
assert(el.textContent?.includes("Manually enabled"));
assert.equal(saved.length, 0, "draft must not write config");
// Toggle enablement without losing this editor's image choice.
const modelCheckbox = el.querySelector<HTMLInputElement>('.provider-model-draft__model input')!;
await act(async () => modelCheckbox.click());
assert(modeInput("auto").disabled && modeInput("on").disabled && modeInput("off").disabled);
await act(async () => modelCheckbox.click());
assert(modeInput("on").checked);
await click("Save");
assert.equal(saved[0].modelOverrides?.[0].vision, true);
assert.equal(saved[0].modelOverrides?.[0].contextWindow, 123456);
assert.equal(saved[0].modelOverrides?.[0].maxOutputTokens, 4321);
await choose("auto");
await click("Save");
assert.equal(saved[1].modelOverrides?.[0].vision, null);
assert.equal(saved[1].modelOverrides?.[0].reasoningProtocol, "openai");
await choose("off");
await click("Cancel");
assert.equal(cancelled, 1);
assert.equal(saved.length, 2);

await render(<ProviderEditor key="legacy-backend" initial={{ ...initial, modelCapabilities: undefined, modelOverrides: [{ ...initial.modelOverrides![0], vision: true }] }} kinds={["openai"]} busy={false} onSave={() => {}} onCancel={() => {}} />);
await choose("auto");
assert(el.textContent?.includes("Image capability unknown"), "auto must not reuse the previous explicit override as its fallback");

// A controlled Promise completes only after unmount. It must not modify the
// replacement editor or enable a model from the previous provider request.
let complete!: (v: ProviderModelCapabilityView[]) => void;
window.go = { main: { App: { FetchProviderModelCatalog: () => new Promise((resolve) => { complete = resolve; }) } as Partial<AppBindings> as AppBindings } };
await render(editor("fetching"));
await click("Test and fetch models");
await render(editor("replacement"));
await act(async () => { complete([{ ...unknown, model: "stale-model", state: "supported" }]); });
assert(!el.textContent?.includes("stale-model"));
assert(modeInput("auto").checked);

await render(<ModelImageInputControl model="relay-model" mode="auto" capability={{ ...unknown, state: "unsupported", automaticState: "unsupported", imageInputEnableAllowed: false, imageInputBlockReason: "official_deepseek_text_model" }} disabled={false} onChange={() => {}} />);
assert(modeInput("on").disabled);
assert(!modeInput("off").disabled);
assert.equal(imageInputState("auto", { ...unknown, automaticState: undefined, source: "override", state: "supported" }), "unknown", "old backend must not fabricate automatic support");
assert.equal(imageInputState("on", { ...unknown, imageInputEnableAllowed: false }), "unsupported");
assert(imageInputHardBlocked("https://api.deepseek.com", "deepseek-v4-flash"));
assert(imageInputHardBlocked("https://eu.deepseek.com/anthropic", "future-vision"));
assert(!imageInputHardBlocked("https://api.deepseek.com.relay.test", "deepseek-v4-flash"));
assert(!imageInputHardBlocked("https://api.deepseek.com", "deepseek-v4-flash-vision-exp"));
assert.equal(mergeImageInputModes(initial.modelOverrides, initial.models, { "RELAY-MODEL": "off" })[0].vision, false);

await render(<ProviderEditorModelPicker candidates={["relay-model"]} selectedModels={["relay-model"]} visionModels={[]} modelCapabilities={[unknown]} imageModes={{ "relay-model": "on" }} onImageInputChange={() => {}} contextWindows={{}} disabled={false} onToggleModel={() => {}} onContextWindowChange={() => {}} onSelectAll={() => {}} onClear={() => {}} />);
assert(el.textContent?.includes("Manually enabled"));
await act(async () => root.unmount());
dom.window.close();
console.log("PASS provider image input: draft/save/auto/cancel/disabled/hard-limit/stale discovery");
