import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import React, { act } from "react";

const dom = new JSDOM('<div id="root"></div><button id="outside">Outside</button>', { url: "http://localhost", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, Node: dom.window.Node,
  HTMLElement: dom.window.HTMLElement, IS_REACT_ACT_ENVIRONMENT: true });
window.matchMedia = (() => ({ matches: true, addEventListener() {}, removeEventListener() {} })) as any;
dom.window.HTMLElement.prototype.scrollIntoView = function () {};
const { createRoot } = await import("react-dom/client");
const { SettingsSelect } = await import("../components/SettingsSelect");
const root = createRoot(document.getElementById("root")!);
const changes: string[] = [];
const options = [
  { value: "one/shared", label: "shared", group: "one", groupLabel: "Connection one" },
  { value: "unsupported", label: "Unsupported", disabled: true },
  { value: "two/shared", label: "shared", group: "two", groupLabel: "Connection two" },
];
async function render(search = false, disabled = false) {
  await act(async () => root.render(<SettingsSelect aria-label="Model" name="model" value="one/shared"
    options={options} disabled={disabled} searchPlaceholder={search ? "Search models" : undefined}
    emptyLabel="No matches" onValueChange={value => changes.push(value)} />));
}
const trigger = () => document.querySelector<HTMLButtonElement>(".settings-select")!;
const list = () => document.querySelector<HTMLElement>('[role="listbox"]')!;
async function key(node: HTMLElement, value: string) {
  await act(async () => node.dispatchEvent(new dom.window.KeyboardEvent("keydown", {key: value, bubbles: true, cancelable: true})));
}
await render();
await key(trigger(), "ArrowDown");
assert.equal(document.activeElement, list(), "plain selector opens with keyboard focus in its list");
await key(list(), "ArrowDown");
assert.equal(document.getElementById(list().getAttribute("aria-activedescendant")!)?.dataset.value, "two/shared", "keyboard navigation skips disabled options");
await key(list(), "Enter");
assert.deepEqual(changes, ["two/shared"], "identical labels preserve connection identity");
assert.equal(document.activeElement, trigger(), "selection returns focus to the trigger");
await act(async () => trigger().click());
await key(list(), "Escape");
assert.equal(trigger().getAttribute("aria-expanded"), "false");
assert.equal(changes.length, 1, "escape does not save");
await act(async () => trigger().click());
await act(async () => document.getElementById("outside")!.click());
assert.equal(trigger().getAttribute("aria-expanded"), "false", "outside click closes menu");
await render(true);
await act(async () => trigger().click());
const search = document.querySelector<HTMLInputElement>('[role="combobox"]')!;
async function searchFor(value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")!.set!.call(search, value);
    search.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  });
}
const beforeComposition = changes.length;
for (const key of ["ArrowDown", "Enter", "Escape"]) {
  const event = new dom.window.KeyboardEvent("keydown", { key, isComposing: true, bubbles: true, cancelable: true });
  await act(async () => { search.dispatchEvent(event); });
  assert.equal(event.defaultPrevented, false, "IME owns candidate keys");
}
assert.equal(trigger().getAttribute("aria-expanded"), "true", "IME confirmation/cancellation keeps the dropdown open");
assert.equal(document.getElementById(search.getAttribute("aria-activedescendant")!)?.dataset.value, "one/shared", "IME arrows do not change the active option");
await key(search, "ArrowDown");
await act(async () => { search.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: "Enter", keyCode: 229, bubbles: true, cancelable: true })); });
assert.equal(changes.length, beforeComposition, "WebKit IME confirmation does not select a model");
assert.equal(trigger().getAttribute("aria-expanded"), "true", "WebKit confirmation keeps the dropdown open");
await searchFor("Connection two");
assert.equal(document.querySelectorAll('[role="option"]').length, 1, "search matches connection labels");
await key(search, "Enter");
assert.equal(changes.at(-1), "two/shared");
await act(async () => trigger().click());
assert.equal(document.querySelector<HTMLInputElement>('[role="combobox"]')!.value, "", "reopening resets search");
await key(document.querySelector<HTMLElement>('[role="combobox"]')!, "Escape");
await render(false, true);
await act(async () => trigger().click());
assert.equal(trigger().getAttribute("aria-expanded"), "false", "disabled selector cannot open");
assert.equal(document.querySelector<HTMLInputElement>('input[name="model"]')!.disabled, true);
await act(async () => root.unmount());
console.log("PASS shared settings select: keyboard, disabled, search, connection identity, dismiss and focus");
