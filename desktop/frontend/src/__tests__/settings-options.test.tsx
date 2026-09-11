import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import React, { act } from "react";
const dom = new JSDOM('<div id="root"></div>');
Object.assign(globalThis, { window: dom.window, document: dom.window.document, HTMLElement: dom.window.HTMLElement, IS_REACT_ACT_ENVIRONMENT: true });
const { createRoot } = await import("react-dom/client");
const { SettingsOptions } = await import("../components/SettingsOptions");
const root = createRoot(document.getElementById("root")!);
const saved: number[] = [];
await act(async () => root.render(<form><SettingsOptions layout="field" aria-label="Language">
  {[0, 1, 2].map(value => <button key={value} disabled={value === 1} className={value === 0 ? "set-seg__btn--on" : ""} onClick={() => saved.push(value)}>{value}</button>)}
</SettingsOptions></form>));
const buttons = Array.from(document.querySelectorAll('button'));
assert.equal(buttons[0].getAttribute('aria-pressed'), 'true');
assert.ok(document.querySelector('.settings-options--field'), 'standard settings choices opt into the shared field width');
assert.equal(buttons[2].getAttribute('aria-pressed'), 'false');
assert.equal(buttons[0].type, 'button', 'choices must not accidentally submit settings forms');
async function key(button: HTMLButtonElement, value: string) {
 await act(async () => button.dispatchEvent(new dom.window.KeyboardEvent('keydown', {key:value,bubbles:true,cancelable:true})));
}
buttons[0].focus();
await key(buttons[0], 'ArrowRight');
assert.equal(document.activeElement, buttons[2]);
assert.deepEqual(saved, [2], 'skip disabled choice and save exactly once');
await key(buttons[2], 'ArrowRight');
assert.equal(document.activeElement, buttons[0], 'wrap at end');
await key(buttons[0], 'End');
assert.equal(document.activeElement, buttons[2]);
await key(buttons[2], 'Home');
assert.equal(document.activeElement, buttons[0]);
await act(async () => root.unmount());
console.log('SettingsOptions keyboard, disabled state, selection semantics and form behavior passed');
