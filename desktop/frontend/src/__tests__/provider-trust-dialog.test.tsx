// Run: tsx src/__tests__/provider-trust-dialog.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";

import type { AppBindings } from "../lib/bridge";

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

console.log("\nRemote Provider Broker authorization");
const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.HTMLElement = dom.window.HTMLElement;

const prompt = {
  hostId: "lab",
  host: "dev@lab.test",
  keyType: "ssh-ed25519",
  fingerprint: "SHA256:verified-host",
  workspace: "/srv/app",
  providerRefs: ["deepseek/deepseek-chat"],
  warning: "Local credentials stay on this machine.",
};
const answers: boolean[] = [];
window.go = { main: { App: {
  async WorkbenchPendingProviderTrust() { return prompt; },
  async WorkbenchResolveProviderTrust(accept: boolean) { answers.push(accept); },
} as Partial<AppBindings> as AppBindings } };
window.runtime = {
  EventsOn() { return () => {}; },
  BrowserOpenURL() {},
};

const [{ createRoot }, { ProviderTrustDialog }, { LocaleProvider }] = await Promise.all([
  import("react-dom/client"),
  import("../components/ProviderTrustDialog"),
  import("../lib/i18n"),
]);
const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);
await act(async () => {
  root.render(<LocaleProvider><ProviderTrustDialog /></LocaleProvider>);
  await Promise.resolve();
});

ok(document.body.textContent?.includes("dev@lab.test") === true, "dialog identifies the verified host");
ok(document.body.textContent?.includes("SHA256:verified-host") === true, "dialog shows the transport fingerprint");
ok(document.body.textContent?.includes("deepseek/deepseek-chat") === true, "dialog scopes authorization to model refs");
ok(document.body.textContent?.includes("API_KEY") === false, "dialog contains no credential material");

const buttons = Array.from(document.querySelectorAll<HTMLButtonElement>("button"));
const accept = buttons.at(-1);
await act(async () => {
  accept?.click();
  await Promise.resolve();
});
ok(answers.length === 1 && answers[0] === true, "authorization answer is sent exactly once");
ok(document.querySelector('[aria-labelledby="provider-trust-title"]') === null, "resolved prompt closes");

await act(async () => root.unmount());
dom.window.close();
process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
