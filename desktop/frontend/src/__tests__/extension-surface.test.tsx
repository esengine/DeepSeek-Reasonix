// Run: tsx src/__tests__/extension-surface.test.tsx
// Stage 8b2: extension_surface / extension_status wire events → per-tab
// controller state, ExtensionCard / ExtensionFormDialog rendering and wiring.

import { JSDOM } from "jsdom";
import { registerHooks } from "node:module";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";

// ExtensionCard lazy-loads the shared MarkdownRenderer, which transitively
// imports katex CSS (and SVG assets); tsx has no asset loader, so redirect
// those specifiers to an empty-string stub, the way Vite would handle them.
registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.endsWith(".css") || specifier.endsWith(".svg")) {
      return nextResolve("./asset-stub-for-tests.ts", { ...context, parentURL: import.meta.url });
    }
    return nextResolve(specifier, context);
  },
});

import { LocaleProvider } from "../lib/i18n";
import type { WireEvent, WireExtensionCard, WireExtensionSurface } from "../lib/types";
import {
  acceptsExtensionGeneration,
  initialState,
  reducer,
  type ExtensionItem,
} from "../lib/useController";
import { ExtensionCard } from "../components/ExtensionCard";
import { ExtensionFormDialog } from "../components/ExtensionFormDialog";

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

// State is not exported from useController; derive it from the reducer.
type ControllerState = Parameters<typeof reducer>[0];

// ── Reducer / store logic ────────────────────────────────────────────────────

function surfaceEvent(partial: Partial<WireExtensionSurface> & Pick<WireExtensionSurface, "kind">, eventKind?: "extension_surface" | "extension_status"): WireEvent {
  return {
    kind: eventKind ?? "extension_surface",
    extension: { pluginId: "alpha", surfaceId: "s1", ...partial },
  };
}

function extensionItems(s: ControllerState): ExtensionItem[] {
  return s.items.filter((it): it is ExtensionItem => it.kind === "extension");
}

console.log("\nExtension surface reducer");

ok(acceptsExtensionGeneration(undefined, 3) && acceptsExtensionGeneration(3, 3) && acceptsExtensionGeneration(3, 4), "accepts new/equal/newer generations");
ok(!acceptsExtensionGeneration(5, 4), "rejects an older generation");
ok(acceptsExtensionGeneration(5, undefined), "events without a generation always pass");

{
  let s: ControllerState = { ...initialState };
  const ev = surfaceEvent({ kind: "status", status: { label: "working", detail: "half", severity: "warn", progress: 0.5 }, generation: 2 }, "extension_status");
  s = reducer(s, { type: "event", e: ev });
  const entry = s.extensionStatuses["alpha:s1"];
  ok(Boolean(entry) && entry.label === "working" && entry.severity === "warn" && entry.progress === 0.5, "extension_status upserts a status entry");
  ok(s.extensionGenerations["alpha:s1"] === 2, "accepted generation is recorded");

  // A second plugin's status keeps the first; a same-key publish replaces it.
  s = reducer(s, { type: "event", e: { kind: "extension_status", extension: { pluginId: "beta", surfaceId: "s1", kind: "status", status: { label: "beta" } } } });
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "status", status: { label: "done", severity: "info" }, generation: 3 }, "extension_status") });
  ok(Object.keys(s.extensionStatuses).length === 2, "statuses key on pluginId:surfaceId");
  ok(s.extensionStatuses["alpha:s1"]?.label === "done", "same-key status replaces in place");

  // extension_surface carrying kind=status reduces identically.
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "status", status: { label: "again" }, generation: 4 }) });
  ok(s.extensionStatuses["alpha:s1"]?.label === "again", "extension_surface kind=status also updates the entry");
}

{
  let s: ControllerState = { ...initialState };
  const card: WireExtensionCard = { title: "Report", text: "v1", fields: [{ key: "k", value: "v" }], progress: 0.25, actions: [{ actionId: "run", label: "Run" }] };
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "card", card, generation: 5 }) });
  ok(extensionItems(s).length === 1 && extensionItems(s)[0].card.title === "Report", "card appends one transcript item");
  const firstId = extensionItems(s)[0].id;

  // Same surface re-published: replace in place, no duplicate.
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "card", card: { ...card, text: "v2" }, generation: 6 }) });
  ok(extensionItems(s).length === 1 && extensionItems(s)[0].id === firstId && extensionItems(s)[0].card.text === "v2", "card re-publish replaces in place");

  // Stale generation: dropped entirely.
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "card", card: { ...card, text: "stale" }, generation: 3 }) });
  ok(extensionItems(s)[0].card.text === "v2" && s.extensionGenerations["alpha:s1"] === 6, "stale card generation is dropped");

  // Equal generation is a legitimate re-publish.
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "card", card: { ...card, text: "v3" }, generation: 6 }) });
  ok(extensionItems(s)[0].card.text === "v3", "equal generation replaces");

  // A different surface id appends its own card.
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "card", surfaceId: "s2", card: { title: "Other" } }) });
  ok(extensionItems(s).length === 2, "different surface id appends a separate card");
}

{
  let s: ControllerState = { ...initialState };
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "form", surfaceId: "f1", generation: 9, form: { title: "Setup", fields: [{ key: "name", kind: "input", required: true }] } }) });
  ok(s.extensionForm?.pluginId === "alpha" && s.extensionForm.form.title === "Setup", "form surface arms the pending form");
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "form", surfaceId: "f1", generation: 4, form: { title: "Stale", fields: [] } }) });
  ok(s.extensionForm?.form.title === "Setup", "stale form generation is dropped");
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "form", surfaceId: "f2", generation: 10, form: { title: "Next", fields: [] } }) });
  ok(s.extensionForm?.surfaceId === "f2", "a new form replaces the pending one");
  s = reducer(s, { type: "clearExtensionForm" });
  ok(s.extensionForm === undefined, "clearExtensionForm dismisses the form");
}

{
  let s: ControllerState = { ...initialState };
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "notification", surfaceId: "n1", notification: { title: "Heads up", body: "b", severity: "warn" } }) });
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "notification", surfaceId: "n2", notification: { title: "Second" } }) });
  ok(s.extensionNotifications.length === 2 && s.extensionNotifications[0].severity === "warn", "notifications queue for the toast drain");
  ok(s.extensionNotifications[0].id !== s.extensionNotifications[1].id, "notification ids are unique");
  s = reducer(s, { type: "extension_notifications_drained" });
  ok(s.extensionNotifications.length === 0, "drain clears the notification queue");
}

{
  let s: ControllerState = { ...initialState };
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "status", status: { label: "working" }, generation: 2 }, "extension_status") });
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "notification", notification: { title: "n" } }) });
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "form", surfaceId: "f1", form: { fields: [] } }) });
  s = reducer(s, { type: "controller_rebuilt" });
  ok(
    Object.keys(s.extensionStatuses).length === 0 && s.extensionForm === undefined && s.extensionNotifications.length === 0 && Object.keys(s.extensionGenerations).length === 0,
    "controller_rebuilt clears extension state and the generation fence",
  );
  // Post-rebuild generations restart from zero: an old high generation must not block.
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "card", card: { title: "fresh" }, generation: 1 }) });
  ok(extensionItems(s).length === 1, "post-rebuild surfaces flow with fresh generations");
}

{
  let s: ControllerState = { ...initialState };
  s = reducer(s, { type: "user", text: "hello", seq: 0, submissionId: "extension-submit" });
  ok(s.pendingUser === "hello", "optimistic user bubble pending");
  s = reducer(s, { type: "event", e: surfaceEvent({ kind: "card", card: { title: "bg" } }) });
  ok(s.pendingUser === "hello", "extension events never flush the optimistic user bubble");
}

{
  const s: ControllerState = { ...initialState };
  const next = reducer(s, { type: "event", e: surfaceEvent({ kind: "mystery" }) });
  ok(next === s, "unknown surface kinds leave state untouched");
  const missingPayload = reducer(s, { type: "event", e: { kind: "extension_surface" } });
  ok(missingPayload === s, "events without an extension payload leave state untouched");
}

// ── Components ───────────────────────────────────────────────────────────────

console.log("\nExtensionCard / ExtensionFormDialog components");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
// Node's built-in navigator reflects the machine's ICU locale; pin jsdom's
// en-US one so English-string assertions hold on zh-locale machines.
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
// tsx loads react-dom before any DOM exists, so isInputEventSupported is false
// and text-input onChange rides the IE polyfill: it only synthesizes change
// for the watched (focused) element on keyup/keydown, via attachEvent.
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });

// setTextInput drives a controlled React text input under the polyfill path:
// focus (starts the polyfill's value watcher), bypass React's value tracker
// with the prototype setter, then keyup to synthesize the change event.
function setTextInput(dom: JSDOM, input: HTMLInputElement, value: string) {
  input.focus();
  const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")?.set;
  setter?.call(input, value);
  input.dispatchEvent(new dom.window.KeyboardEvent("keyup", { key: ".", bubbles: true }));
}

async function flush(ms = 30) {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

const invokeCalls: Array<{ tabId: string; name: string; args: Record<string, string> }> = [];
let invokeResult: string | Error = "Completed!";
(dom.window as unknown as { go: unknown }).go = {
  main: {
    App: {
      InvokeExtensionAction: async (tabId: string, name: string, args: Record<string, string>) => {
        invokeCalls.push({ tabId, name, args });
        if (invokeResult instanceof Error) throw invokeResult;
        return invokeResult;
      },
    },
  },
};

const cardItem: ExtensionItem = {
  kind: "extension",
  id: "x0",
  surfaceKey: "alpha:c1",
  pluginId: "alpha",
  surfaceId: "c1",
  generation: 3,
  card: {
    title: "Weekly report",
    markdown: "**bold** body",
    fields: [
      { key: "range", value: "7d" },
      { key: "rows", value: "42" },
    ],
    progress: 0.4,
    actions: [{ actionId: "run", label: "Run now" }],
  },
};

{
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => {
    root.render(
      <LocaleProvider>
        <ExtensionCard item={cardItem} tabId="tab-1" />
      </LocaleProvider>,
    );
    await flush(120);
  });

  ok(container.textContent?.includes("Weekly report") === true, "card renders the title");
  ok(container.querySelector(".extension-card__plugin")?.textContent === "alpha", "card badges the plugin id");
  ok(container.querySelectorAll(".extension-card__field").length === 2, "card renders the key-value grid");
  ok(container.querySelector(".extension-card__progress")?.getAttribute("aria-valuenow") === "40", "card renders progress as an accessible progressbar");
  ok(container.querySelector(".extension-card__body")?.querySelector("strong") !== null || container.textContent?.includes("bold") === true, "card renders markdown through the shared renderer");

  const runButton = Array.from(container.querySelectorAll<HTMLButtonElement>(".extension-card__actions button")).find((b) => b.textContent === "Run now");
  await act(async () => {
    runButton?.click();
    await flush();
  });
  ok(invokeCalls.length === 1 && invokeCalls[0].tabId === "tab-1" && invokeCalls[0].name === "/alpha:run", "action click invokes /<plugin>:<action> on the tab");
  ok(container.querySelector(".extension-card__result")?.textContent === "Completed!", "action result message renders inline");
  ok(container.querySelector(".extension-card__result--error") === null, "successful result is not styled as an error");

  invokeResult = new Error("sidecar exploded");
  await act(async () => {
    runButton?.click();
    await flush();
  });
  ok(invokeCalls.length === 2, "failed action still reached the bridge");
  ok(container.querySelector(".extension-card__result--error")?.textContent === "sidecar exploded", "action failure renders an inline error");

  await act(async () => root.unmount());
  container.remove();
}

// ── ExtensionFormDialog ──────────────────────────────────────────────────────

{
  const submitted: Array<Record<string, unknown>> = [];
  let cancels = 0;
  const container = document.createElement("div");
  document.body.appendChild(container);
  const root = createRoot(container);
  const surface = {
    pluginId: "alpha",
    surfaceId: "f1",
    generation: 9,
    form: {
      title: "Configure sync",
      fields: [
        { key: "agree", label: "I agree", kind: "confirm" },
        { key: "name", label: "Name", kind: "input", required: true },
        { key: "mode", label: "Mode", kind: "select", options: ["fast", "safe"], default: "fast" },
        { key: "scopes", label: "Scopes", kind: "multiselect", options: ["mail", "cal"], required: true },
      ],
    },
  };
  await act(async () => {
    root.render(
      <LocaleProvider>
        <ExtensionFormDialog surface={surface} onSubmit={(values) => submitted.push(values)} onCancel={() => { cancels += 1; }} />
      </LocaleProvider>,
    );
    await flush();
  });

  ok(container.textContent?.includes("Configure sync") === true, "form renders its title");
  ok(container.querySelector(".prompt-shelf__badge")?.textContent === "alpha", "form badges the plugin id");
  const submitButton = () => Array.from(container.querySelectorAll<HTMLButtonElement>("button")).find((b) => b.textContent === "Submit");
  ok(submitButton()?.disabled === true, "required fields keep submit disabled");

  const nameInput = container.querySelector<HTMLInputElement>(".extension-form__input");
  await act(async () => {
    if (nameInput) setTextInput(dom, nameInput, "wen");
    await flush();
  });
  ok(submitButton()?.disabled === true, "still disabled while the required multiselect is empty");

  const calOption = Array.from(container.querySelectorAll<HTMLButtonElement>(".extension-form__options button")).find((b) => b.textContent === "cal");
  await act(async () => {
    calOption?.click();
    await flush();
  });
  ok(submitButton()?.disabled === false, "submit enables once required fields are filled");

  await act(async () => {
    submitButton()?.click();
    await flush();
  });
  const values = submitted[0];
  ok(
    submitted.length === 1 &&
      values?.agree === false &&
      values?.name === "wen" &&
      values?.mode === "fast" &&
      Array.isArray(values?.scopes) &&
      (values.scopes as string[]).join(",") === "cal",
    "submit delivers typed values (bool / string / select default / multiselect)",
  );

  await act(async () => {
    document.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await flush();
  });
  ok(cancels === 1, "Escape cancels the form");

  await act(async () => root.unmount());
  container.remove();
}

console.log(`\n${passed} passed, ${failed} failed`);
dom.window.close();
if (failed > 0) process.exit(1);
