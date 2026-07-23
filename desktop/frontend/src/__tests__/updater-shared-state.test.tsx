// Run: tsx src/__tests__/updater-shared-state.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { UpdaterProvider, useUpdater } from "../lib/useUpdater";

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

function Consumer({ id, checking = false }: { id: string; checking?: boolean }) {
  const updater = useUpdater();
  return (
    <section>
      <output id={`${id}-status`}>{updater.status.kind}</output>
      {checking && <button id="check-update" type="button" onClick={() => void updater.check()}>Check</button>}
    </section>
  );
}

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.MouseEvent = dom.window.MouseEvent;

const root = createRoot(document.getElementById("root")!);
await act(async () => {
  root.render(
    <UpdaterProvider>
      <Consumer id="banner" checking />
      <Consumer id="settings" />
    </UpdaterProvider>,
  );
});

ok(document.getElementById("banner-status")?.textContent === "idle", "banner starts idle");
ok(document.getElementById("settings-status")?.textContent === "idle", "settings starts idle");

await act(async () => {
  (document.getElementById("check-update") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});

ok(document.getElementById("banner-status")?.textContent === "upToDate", "banner receives the check result");
ok(document.getElementById("settings-status")?.textContent === "upToDate", "settings receives the same check result");

await act(async () => root.unmount());

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
