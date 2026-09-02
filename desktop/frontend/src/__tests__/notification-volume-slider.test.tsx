// Run: tsx src/__tests__/notification-volume-slider.test.tsx

import React, { act } from "react";
import { JSDOM } from "jsdom";

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

console.log("\nnotification volume slider");
const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;

const [{ createRoot }, { NotificationVolumeSlider }, { LocaleProvider }] = await Promise.all([
  import("react-dom/client"),
  import("../components/NotificationVolumeSlider"),
  import("../lib/i18n"),
]);

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);
const changes: number[] = [];
await act(async () => {
  root.render(
    <LocaleProvider>
      <NotificationVolumeSlider value={70} onChange={(value) => changes.push(value)} />
    </LocaleProvider>,
  );
});

const input = document.querySelector<HTMLInputElement>('input[type="range"]');
const output = document.querySelector<HTMLOutputElement>("output");
ok(input?.getAttribute("aria-label") === "Notification volume", "slider exposes its localized purpose");
ok(input?.min === "0" && input?.max === "100" && input?.step === "5", "slider exposes the full 0–100% keyboard range");
ok(input?.value === "70" && input?.getAttribute("aria-valuetext") === "70%", "slider announces the default percentage");
ok(output?.textContent === "70%" && output?.getAttribute("for") === input?.id, "visible percentage is associated with the slider");
await act(async () => {
  if (!input) return;
  const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")?.set;
  setter?.call(input, "85");
  input.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  input.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
});
ok(changes.at(-1) === 85, "dragging or using the keyboard emits the next percentage");

await act(async () => root.unmount());
dom.window.close();
process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
