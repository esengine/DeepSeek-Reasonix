// Run: tsx src/__tests__/blank-project-dialog.test.tsx

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";
import React, { act } from "react";
import type { AppBindings } from "../lib/bridge";

let passed = 0;
let failed = 0;
function ok(value: boolean, label: string) {
  if (value) {
    passed += 1;
    process.stdout.write(`  PASS  ${label}\n`);
  } else {
    failed += 1;
    process.stdout.write(`  FAIL  ${label}\n`);
  }
}

console.log("\nblank project creation");
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
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
Object.defineProperty(dom.window.HTMLElement.prototype, "attachEvent", { configurable: true, value: () => {} });
Object.defineProperty(dom.window.HTMLElement.prototype, "detachEvent", { configurable: true, value: () => {} });

const [{ createRoot }, { BlankProjectDialog, blankProjectNameProblem }, { BlankProjectFlow }, { LocaleProvider }] = await Promise.all([
  import("react-dom/client"),
  import("../components/BlankProjectDialog"),
  import("../components/BlankProjectFlow"),
  import("../lib/i18n"),
]);

ok(blankProjectNameProblem("   ") === "required", "empty names are rejected before IPC");
ok(blankProjectNameProblem("nested/project") === "invalid", "nested paths are rejected before IPC");
ok(blankProjectNameProblem("project") === null, "a single folder name is accepted");

const submitted: string[] = [];
let cancelled = 0;
const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);
await act(async () => {
  root.render(
    <LocaleProvider>
      <BlankProjectDialog
        parentDirectory="/Users/test/Projects"
        busy={false}
        onSubmit={(name) => submitted.push(name)}
        onCancel={() => { cancelled += 1; }}
      />
    </LocaleProvider>,
  );
});

const dialog = document.querySelector<HTMLElement>('[role="dialog"]');
const input = document.querySelector<HTMLInputElement>(".blank-project-dialog__input");
const submit = document.querySelector<HTMLButtonElement>('button[type="submit"]');
ok(Boolean(dialog) && dialog?.getAttribute("aria-modal") === "true", "creation uses an accessible modal dialog");
ok(document.body.textContent?.includes("/Users/test/Projects") === true, "the chosen parent folder is shown before creation");
ok(document.activeElement === input, "project name receives initial focus");
ok(submit?.disabled === true, "create stays disabled until the name is valid");

await act(async () => {
  if (!input) return;
  const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")?.set;
  setter?.call(input, "  demo-project  ");
  input.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  input.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
});
ok(submit?.disabled === false, "a valid project name enables creation");
await act(async () => {
  dialog?.querySelector("form")?.dispatchEvent(new dom.window.Event("submit", { bubbles: true, cancelable: true }));
});
ok(submitted[0] === "demo-project", "submission trims and forwards the project name");

await act(async () => {
  document.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
});
ok(cancelled === 1, "Escape cancels the dialog");

const here = dirname(fileURLToPath(import.meta.url));
const treeSource = readFileSync(resolve(here, "../components/ProjectTree.tsx"), "utf8");
const flowSource = readFileSync(resolve(here, "../components/BlankProjectFlow.tsx"), "utf8");
const creationSource = readFileSync(resolve(here, "../components/useProjectCreation.tsx"), "utf8");
ok(/key: "blank-project"[\s\S]*openBlankProjectFlow\(\)/.test(treeSource), "workbench menu starts the blank-project flow");
ok(/CreateBlankProject\(draft\.parentDirectory, projectName\)/.test(flowSource), "the dialog delegates atomic directory creation to Go");
ok(/await onOpenProject\(createdPath\)/.test(flowSource), "the new directory reuses workspace navigation and registration");
ok(/lazy\(\(\) => import\("\.\/BlankProjectFlow"\)/.test(creationSource), "the creation flow stays outside the startup bundle");

await act(async () => root.unmount());

const bridgeCalls: string[] = [];
window.go = { main: { App: {
  async PickBlankProjectParent() {
    bridgeCalls.push("pick");
    return "/Users/test/Projects";
  },
  async CreateBlankProject(parentDirectory: string, projectName: string) {
    bridgeCalls.push(`create:${parentDirectory}:${projectName}`);
    return `${parentDirectory}/${projectName}`;
  },
} as Partial<AppBindings> as AppBindings } };
const flowRoot = createRoot(rootElement);
await act(async () => {
  flowRoot.render(
    <LocaleProvider>
      <BlankProjectFlow
        onOpenProject={async (path) => { bridgeCalls.push(`open:${path}`); }}
        onRefresh={async () => { bridgeCalls.push("refresh"); }}
        onClose={() => { bridgeCalls.push("close"); }}
      />
    </LocaleProvider>,
  );
  await Promise.resolve();
  await Promise.resolve();
});
const flowInput = document.querySelector<HTMLInputElement>(".blank-project-dialog__input");
await act(async () => {
  if (!flowInput) return;
  const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")?.set;
  setter?.call(flowInput, "flow-project");
  flowInput.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  flowInput.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
});
await act(async () => {
  flowInput?.closest("form")?.dispatchEvent(new dom.window.Event("submit", { bubbles: true, cancelable: true }));
  await Promise.resolve();
  await Promise.resolve();
});
ok(
  bridgeCalls.join("|") === "pick|create:/Users/test/Projects:flow-project|open:/Users/test/Projects/flow-project|refresh|close",
  "the live flow picks, creates, opens, refreshes, and closes in order",
);

await act(async () => flowRoot.unmount());
dom.window.close();
process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
