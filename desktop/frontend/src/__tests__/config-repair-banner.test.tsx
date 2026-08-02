// Run: tsx src/__tests__/config-repair-banner.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";

import type { AppBindings } from "../lib/bridge";
import { t } from "../lib/i18n";
import type { ConfigRepairView } from "../lib/types";

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

console.log("\nConfig repair banner bindings");
const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.HTMLElement = dom.window.HTMLElement;

const empty: ConfigRepairView = {
  outcome: "",
  scope: "",
  path: "",
  detail: "",
  repairedAt: "",
  undoable: false,
  canOpenFile: false,
};
const damaged: ConfigRepairView = {
  ...empty,
  outcome: "config_damaged",
  scope: "global",
  detail: "damaged config",
  canOpenFile: true,
};
let status = damaged;
const undoIDs: string[] = [];
const bindings = {
  async ConfigRepairStatus() { return status; },
  async UndoConfigRepair(transactionID: string) { undoIDs.push(transactionID); },
  async OpenConfigFile() {},
  async RestoreGlobalConfigSnapshot() { return false; },
} as Partial<AppBindings> as AppBindings;
window.go = { main: { App: bindings } };

const [{ createRoot }, { ConfigRepairBanner }] = await Promise.all([
  import("react-dom/client"),
  import("../components/ConfigRepairBanner"),
]);
const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);
await act(async () => {
  root.render(<ConfigRepairBanner api={bindings} t={t} />);
  await Promise.resolve();
});
ok(document.body.textContent?.includes("damaged config") === true, "damaged config is surfaced");

status = empty;
await act(async () => {
  window.dispatchEvent(new dom.window.Event("focus"));
  await Promise.resolve();
});
ok(document.querySelector(".config-repair-banner") === null, "manual repair clears the banner on live refresh");

await act(async () => root.unmount());
const secondRoot = createRoot(rootElement);
status = {
  ...empty,
  outcome: "auto_fixed",
  scope: "global",
  detail: "one path repaired",
  undoable: true,
  transactionId: "repair-visible-token",
};
await act(async () => {
  secondRoot.render(<ConfigRepairBanner api={bindings} t={t} />);
  await Promise.resolve();
});
const undo = Array.from(document.querySelectorAll<HTMLButtonElement>("button"))
  .find((button) => button.textContent?.includes("Undo"));
await act(async () => {
  undo?.click();
  await Promise.resolve();
});
ok(undoIDs.length === 1 && undoIDs[0] === "repair-visible-token", "undo sends the displayed transaction token");

await act(async () => secondRoot.unmount());
dom.window.close();
process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
