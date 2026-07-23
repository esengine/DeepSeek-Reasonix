// Run: tsx src/__tests__/remote-hosts-page.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";

import { RemoteHostsPage } from "../components/RemoteHostsPage";
import type { AppBindings } from "../lib/bridge";
import { LocaleProvider } from "../lib/i18n";
import type { RemoteHostView } from "../lib/types";

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

function button(label: string, root: ParentNode = document): HTMLButtonElement | undefined {
  return Array.from(root.querySelectorAll<HTMLButtonElement>("button"))
    .find((candidate) => candidate.textContent?.trim() === label);
}

console.log("\nRemote SSH host settings");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);

let hosts: RemoteHostView[] = [{
  id: "build-box",
  label: "Build box",
  host: "ssh.example.test",
  port: 22,
  user: "dev",
  identityFile: "~/.ssh/id_ed25519",
  proxyJump: "",
  defaultWorkspace: "/srv/app",
  serveInstall: "auto",
  useSSHConfig: false,
  passwordSet: true,
  keyPassphraseSet: true,
}];
let removeCalls = 0;
const bindings = {
  async RemoteHosts() { return hosts.slice(); },
  async RemoteConnectionStatuses() { return []; },
  async RemoveRemoteHost(id: string) {
    removeCalls += 1;
    hosts = hosts.filter((host) => host.id !== id);
  },
} as unknown as AppBindings;
window.go = { main: { App: bindings } };

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("missing root");
const root = createRoot(rootElement);

await act(async () => {
  root.render(<LocaleProvider><RemoteHostsPage /></LocaleProvider>);
  await flush();
});

await act(async () => {
  button("Remove")?.click();
  await flush();
});
const firstDialog = document.querySelector<HTMLElement>(".reasonix-confirm-dialog");
ok(Boolean(firstDialog), "remove opens the in-app confirmation dialog");
ok(firstDialog?.textContent?.includes("Build box") === true, "confirmation identifies the host being removed");
ok(removeCalls === 0, "host is not removed before confirmation");

await act(async () => {
  button("Cancel", firstDialog ?? document)?.click();
  await flush();
});
ok(removeCalls === 0 && document.querySelector(".reasonix-confirm-dialog") === null, "Cancel keeps the host and closes the dialog");

await act(async () => {
  button("Edit")?.click();
  await flush();
});
const secretInputs = document.querySelectorAll<HTMLInputElement>('input[type="password"]');
ok(secretInputs.length === 2, "edit form exposes password and private-key passphrase fields");
ok(Array.from(secretInputs).every((input) => input.value === "" && input.placeholder.includes("Saved")), "saved credentials are represented without returning plaintext to the UI");

await act(async () => {
  button("Remove saved password")?.click();
  await flush();
});
ok(document.body.textContent?.includes("saved password will be removed") === true, "explicit clear action is staged until Save");

await act(async () => {
  button("Cancel")?.click();
  await flush();
  button("Remove")?.click();
  await flush();
});
const secondDialog = document.querySelector<HTMLElement>(".reasonix-confirm-dialog");
await act(async () => {
  button("Remove", secondDialog ?? document)?.click();
  await flush();
});
ok(removeCalls === 1, "confirmed removal invokes the backend exactly once");
ok(document.body.textContent?.includes("No remote hosts yet") === true, "host list refreshes after removal");

await act(async () => root.unmount());
dom.window.close();

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
