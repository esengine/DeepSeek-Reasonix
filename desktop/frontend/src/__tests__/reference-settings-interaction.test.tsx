// Run: tsx src/__tests__/reference-settings-interaction.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { SettingsPanel } from "../components/SettingsPanel";
import { LocaleProvider } from "../lib/i18n";
import type { AppBindings } from "../lib/bridge";
import type { ReferenceSettingsView, SettingsView } from "../lib/types";
import { baseSettings, flushPromises, installCanvasMock } from "../test-support/settingsTestFixtures";

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

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });
installCanvasMock(dom.window as unknown as Window);
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.localStorage = dom.window.localStorage;
globalThis.sessionStorage = dom.window.sessionStorage;

let settings: SettingsView = baseSettings("standard");
let persisted = settings.reference;
const saved: ReferenceSettingsView[] = [];

window.go = {
  main: {
    App: {
      Settings: async () => ({ ...settings, reference: { ...persisted, excludePatterns: [...persisted.excludePatterns] } }),
      PickReferenceFolder: async () => ".hidden-cache",
      PickReferenceFile: async () => ".env.local",
      SetReferenceSettings: async (view: ReferenceSettingsView) => {
        persisted = { ...view, excludePatterns: [...view.excludePatterns] };
        saved.push(persisted);
      },
    } as Partial<AppBindings> as AppBindings,
  },
};

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(
    <LocaleProvider>
      <SettingsPanel
        initialTab="reference"
        desktopPlatform="linux"
        onClose={() => {}}
        onChanged={() => {}}
        onUseSubagent={() => {}}
      />
    </LocaleProvider>,
  );
  await flushPromises();
});

const toggle = rootEl.querySelector('button[role="switch"]') as HTMLButtonElement | null;
if (!toggle) throw new Error("Git ignore switch did not render");
ok(toggle.getAttribute("aria-checked") === "false", "Git ignore switch starts off");

await act(async () => {
  toggle.click();
  await flushPromises();
  await flushPromises();
});
ok(saved.at(-1)?.followGitignore === true, "clicking Git ignore switch saves the enabled state");
ok(toggle.getAttribute("aria-checked") === "true", "Git ignore switch reflects the refreshed enabled state");

const chooseFile = Array.from(rootEl.querySelectorAll("button")).find((button) => button.textContent?.includes("Choose file"));
if (!chooseFile) throw new Error("Choose file button did not render");
await act(async () => {
  chooseFile.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true }));
  await flushPromises();
  await flushPromises();
});
ok(saved.at(-1)?.excludePatterns.includes(".env.local") === true, "selected hidden file is saved as a file rule");
ok(rootEl.textContent?.includes(".env.local") === true, "selected file is rendered as a rule bubble");

const chooseFolder = Array.from(rootEl.querySelectorAll("button")).find((button) => button.textContent?.includes("Choose folder"));
if (!chooseFolder) throw new Error("Choose folder button did not render");
await act(async () => {
  chooseFolder.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true }));
  await flushPromises();
  await flushPromises();
});
ok(saved.at(-1)?.excludePatterns.includes(".hidden-cache/**") === true, "selected hidden folder is saved as a folder rule");
ok(rootEl.textContent?.includes(".hidden-cache") === true, "selected folder is rendered as a rule bubble");
ok(rootEl.querySelectorAll(".reference-filter-editor input").length === 0, "reference filter has no obsolete mode or text input");

const deleteButtons = rootEl.querySelectorAll(".reference-filter-rule .set-rule__x");
if (deleteButtons.length === 0) throw new Error("rule delete button did not render");
await act(async () => {
  (deleteButtons[0] as HTMLButtonElement).click();
  await flushPromises();
  await flushPromises();
});
ok(saved.at(-1)?.excludePatterns.includes(".env.local") === false, "rule bubble delete saves the removal");
ok(rootEl.textContent?.includes(".env.local") === false, "deleted rule bubble disappears after refresh");

await act(async () => {
  root.unmount();
});
console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
