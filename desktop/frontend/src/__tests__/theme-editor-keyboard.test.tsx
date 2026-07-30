// Run: tsx src/__tests__/theme-editor-keyboard.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { ThemeGallery } from "../components/ThemeGallery";
import { LocaleProvider } from "../lib/i18n";
import type { ThemeExperienceView } from "../lib/themeExperience";
import type { ThemePackView } from "../lib/themePack";

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

async function flush() {
  await new Promise((resolve) => setTimeout(resolve, 20));
}

const experience: ThemeExperienceView = {
  themeMode: "dark",
  baseStyle: "graphite",
  effectiveStyle: "graphite",
  activePack: null,
  safeMode: false,
};

const savedPack: ThemePackView = {
  id: "my-theme",
  name: "My Theme",
  baseStyle: "graphite",
  builtin: false,
  kind: "user",
  active: false,
  hasBackground: false,
  tokens: {},
  recipes: { density: "comfortable", corners: "soft" },
};

console.log("\ntheme editor keyboard ownership");

const dom = new JSDOM("<!doctype html><html><body><button id=\"opener\">Open editor</button><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.HTMLInputElement = dom.window.HTMLInputElement;
globalThis.Event = dom.window.Event;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
(dom.window.HTMLElement.prototype as unknown as { attachEvent: () => void }).attachEvent = () => {};
(dom.window.HTMLElement.prototype as unknown as { detachEvent: () => void }).detachEvent = () => {};

let resolveSave: ((pack: ThemePackView) => void) | null = null;
const pendingSave = new Promise<ThemePackView>((resolve) => {
  resolveSave = resolve;
});
window.go = {
  main: {
    App: {
      ListThemePacks: async () => [],
      SaveThemePack: async () => pendingSave,
    },
  },
};

const rootElement = document.getElementById("root");
const opener = document.getElementById("opener") as HTMLButtonElement | null;
if (!rootElement || !opener) throw new Error("missing test root");
const root = createRoot(rootElement);

function gallery(key: string) {
  return (
    <LocaleProvider>
      <ThemeGallery
        key={key}
        experience={experience}
        initialCreateBaseStyle="graphite"
        onExperienceChange={() => {}}
        onBack={() => {}}
      />
    </LocaleProvider>
  );
}

let outerEscapeCount = 0;
const onOuterEscape = (event: KeyboardEvent) => {
  if (event.key === "Escape") outerEscapeCount += 1;
};
document.addEventListener("keydown", onOuterEscape);

opener.focus();
await act(async () => {
  root.render(gallery("normal"));
  await flush();
});
const normalDialog = document.querySelector<HTMLElement>(".theme-gallery__editor");
if (!normalDialog) throw new Error("theme editor did not render");
const normalEscape = new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true });
await act(async () => {
  normalDialog.dispatchEvent(normalEscape);
  await flush();
});
ok(normalEscape.defaultPrevented, "normal Escape is consumed by the nested editor");
ok(outerEscapeCount === 0, "normal Escape does not reach the outer Settings handler");
ok(document.querySelector(".theme-gallery__editor") === null, "normal Escape closes only the theme editor");
ok(document.activeElement === opener, "normal Escape restores focus to the opener");

opener.focus();
await act(async () => {
  root.render(gallery("busy"));
  await flush();
});
const saveButton = Array.from(document.querySelectorAll<HTMLButtonElement>(".theme-editor__actions button"))
  .find((button) => button.textContent === "Save");
if (!saveButton) throw new Error("save button did not render");
await act(async () => {
  saveButton.click();
  await flush();
});
ok(saveButton.disabled, "save puts the editor into its busy state");
const busyDialog = document.querySelector<HTMLElement>(".theme-gallery__editor");
if (!busyDialog) throw new Error("busy theme editor did not remain mounted");
const busyEscape = new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true });
await act(async () => {
  busyDialog.dispatchEvent(busyEscape);
  await flush();
});
ok(busyEscape.defaultPrevented, "busy Escape is still consumed by the nested editor");
ok(outerEscapeCount === 0, "busy Escape does not close the outer Settings panel");
ok(document.querySelector(".theme-gallery__editor") !== null, "busy Escape keeps the editor mounted until save completes");

await act(async () => {
  resolveSave?.(savedPack);
  await pendingSave;
  await flush();
});

document.removeEventListener("keydown", onOuterEscape);
await act(async () => root.unmount());
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
