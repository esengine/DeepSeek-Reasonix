import { strict as assert } from "node:assert";
import { JSDOM } from "jsdom";

const { window } = new JSDOM('<!doctype html><html><body><button id="trigger">Trigger</button><button id="outside">Outside</button><div id="root"></div></body></html>');
Object.assign(globalThis, {
  window, document: window.document, HTMLElement: window.HTMLElement, Node: window.Node,
  IS_REACT_ACT_ENVIRONMENT: true,
});
const { createElement, act, StrictMode } = await import("react");
const { createRoot } = await import("react-dom/client");
const { ContextMenu } = await import("../components/ContextMenu");
const root = createRoot(document.getElementById("root")!);
const trigger = document.getElementById("trigger")!;
const outside = document.getElementById("outside")!;
const point = { left: 20, top: 20, keyboardTarget: trigger };
let open = true;
let selected = "";
const close = () => { open = false; render(); };
const items = [
  { key: "disabled", label: "Disabled", disabled: true, onSelect() {} },
  { key: "first", label: "First", onSelect() { selected = "first"; close(); } },
  { type: "separator" as const, key: "separator" },
  { key: "last", label: "Last", onSelect() { selected = "last"; close(); } },
];
function render(keyboard = true) {
  root.render(createElement(StrictMode, null, createElement(ContextMenu, {
    open, point: keyboard ? point : { left: 20, top: 20 }, items, onClose: close,
  })));
}
async function key(key: string) {
  await act(async () => {
    document.activeElement!.dispatchEvent(new window.KeyboardEvent("keydown", { key, bubbles: true, cancelable: true }));
  });
}
trigger.focus();
await act(async () => render());
assert.equal(document.activeElement?.textContent, "First", "skip disabled item under StrictMode");
for (const [pressed, expected] of [["ArrowDown", "Last"], ["ArrowDown", "First"], ["ArrowUp", "Last"], ["Home", "First"], ["End", "Last"]]) {
  await key(pressed);
  assert.equal(document.activeElement?.textContent, expected, pressed);
}
await act(async () => (document.activeElement as HTMLButtonElement).click());
assert.equal(selected, "last");
assert.equal(document.querySelector('[role="menu"]'), null);
assert.equal(document.activeElement, trigger, "selection restores focus");
for (const pressed of ["Escape", "Tab"]) {
  open = true;
  await act(async () => render());
  await key(pressed);
  assert.equal(document.activeElement, trigger, pressed);
  assert.equal(document.querySelector('[role="menu"]'), null);
}
open = true;
await act(async () => render());
await act(async () => { outside.focus(); close(); });
assert.equal(document.activeElement, outside, "close does not steal externally moved focus");
trigger.focus();
open = true;
await act(async () => render(false));
assert.equal(document.activeElement, trigger, "pointer menu preserves focus and selection");
await act(async () => { open = false; render(false); });
open = true;
await act(async () => render());
await act(async () => { trigger.remove(); root.unmount(); });
assert.equal(document.querySelector('[role="menu"]'), null, "unmount tolerates removed trigger");
console.log("context-menu keyboard: all assertions passed");
