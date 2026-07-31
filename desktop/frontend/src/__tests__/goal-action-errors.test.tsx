// Run: tsx src/__tests__/goal-action-errors.test.tsx

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { useGoalActionHandler } from "../lib/goalAction";
import { ToastProvider } from "../lib/toast";

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

function flushPromises(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

console.log("\ngoal action bridge errors");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;

const unhandled: unknown[] = [];
const onUnhandledRejection = (reason: unknown) => unhandled.push(reason);
const onWindowUnhandledRejection = (event: PromiseRejectionEvent) => unhandled.push(event.reason);
process.on("unhandledRejection", onUnhandledRejection);
window.addEventListener("unhandledrejection", onWindowUnhandledRejection);

function Probe() {
  const { runGoalAction } = useGoalActionHandler();
  const run = (label: string) => {
    runGoalAction(async () => {
      throw new Error(`${label} bridge failed`);
    });
  };
  return (
    <>
      <button type="button" data-action="stop" onClick={() => run("stop goal")}>stop</button>
      <button type="button" data-action="mode" onClick={() => run("switch mode")}>mode</button>
      <button type="button" data-action="resync" onClick={() => run("background goal resync")}>resync</button>
    </>
  );
}

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);
await act(async () => {
  root.render(<ToastProvider><Probe /></ToastProvider>);
});

for (const action of ["stop", "mode", "resync"]) {
  await act(async () => {
    document.querySelector<HTMLButtonElement>(`[data-action="${action}"]`)?.click();
    await flushPromises();
  });
}

const errors = Array.from(document.querySelectorAll(".toast--error .toast__text")).map((node) => node.textContent);
ok(errors.includes("stop goal bridge failed"), "rejected Stop Goal action shows an error toast");
ok(errors.includes("switch mode bridge failed"), "rejected mode action shows an error toast");
ok(errors.includes("background goal resync bridge failed"), "rejected background Goal resync shows an error toast");
ok(unhandled.length === 0, "handled Goal action rejections do not emit unhandledrejection");

const here = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(here, "../App.tsx"), "utf8");
ok(
  /runGoalAction\(\(\) => applyCollaborationMode\(collaborationMode === "plan" \? "normal" : "plan"\)\)/.test(appSource),
  "mode shortcut routes through the rejection handler",
);
ok(
  /runGoalAction\(async \(\) => \{[\s\S]{0,260}setControllerComposerProfileForTab\([\s\S]{0,260}propagateError: true/.test(appSource),
  "background Goal resync routes through the rejection handler",
);
ok(appSource.includes("onClearGoal={clearGoalFromUi}"), "Composer Stop Goal routes through the rejection handler");
ok(appSource.includes("onSetCollaborationMode={setCollaborationModeFromUi}"), "Composer mode changes route through the rejection handler");
ok(/if \(model\) \{\s*await switchModel\(model\[1\]\);/.test(appSource), "/model awaits Goal restoration failures");
ok(
  /await \(trimmed \? setControllerGoalForTab\(tabId, trimmed\) : clearControllerGoalForTab\(tabId\)\);\s*patchActivatedGoalForTab\(tabId, trimmed\)/.test(appSource),
  "failed Goal bridge calls cannot patch local Goal state or user intent",
);

await act(async () => {
  root.unmount();
});
process.off("unhandledRejection", onUnhandledRejection);
window.removeEventListener("unhandledrejection", onWindowUnhandledRejection);
dom.window.close();

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
