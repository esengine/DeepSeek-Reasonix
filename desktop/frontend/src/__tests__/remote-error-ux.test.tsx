// Run: tsx src/__tests__/remote-error-ux.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";

import { StatusBar } from "../components/StatusBar";
import { LocaleProvider } from "../lib/i18n";
import type { RemoteConnectionStatus, RemoteHostView } from "../lib/types";
import { useRemoteStore } from "../store/remote";

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
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.MouseEvent = dom.window.MouseEvent;
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame.bind(dom.window);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame.bind(dom.window);
Object.defineProperty(window, "matchMedia", {
  configurable: true,
  value: () => ({ matches: true, addEventListener() {}, removeEventListener() {} }),
});

const host: RemoteHostView = {
  id: "box",
  label: "Build box",
  host: "example.test",
  port: 2222,
  user: "dev",
  identityFile: "",
  proxyJump: "",
  defaultWorkspace: "/srv/app",
  serveInstall: "auto",
  useSSHConfig: false,
};
const rawError = "remote: host key mismatch (/home/dev/.ssh/known_hosts:7)";
const status: RemoteConnectionStatus = {
  hostId: "box",
  state: "stopped",
  error: rawError,
  errorDetails: {
    code: "host_key_mismatch",
    presentedSha256: "SHA256:new",
    knownHostRecords: [{ path: "/home/dev/.ssh/known_hosts", line: 7 }],
  },
};
const degradedStatus: RemoteConnectionStatus = {
  hostId: "box",
  state: "degraded",
  error: "forward attach failed",
};

useRemoteStore.setState({ statusPopoverRequest: null });
const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing root");
const root = createRoot(rootEl);

await act(async () => {
  root.render(
    <LocaleProvider>
      <StatusBar
        context={{ used: 0, window: 0, sessionTokens: 0 }}
        running={false}
        remoteHosts={[host]}
        remoteStatuses={{ box: status }}
      />
    </LocaleProvider>,
  );
});

await act(async () => {
  useRemoteStore.getState().requestStatusPopover("box");
  await new Promise((resolve) => requestAnimationFrame(() => resolve(undefined)));
});

const card = document.querySelector<HTMLElement>(".remote-switcher__error-card");
ok(Boolean(card), "terminal connection failure opens the anchored Remote SSH popover");
ok(card?.textContent?.includes("host key differs from the previous record") === true, "popover uses a localized security summary");
ok(card?.textContent?.includes("/home/dev/.ssh/known_hosts") === false, "primary error card hides machine-local diagnostics");

const detailsButton = Array.from(card?.querySelectorAll("button") ?? []).find((button) => button.textContent?.includes("View key details"));
await act(async () => {
  detailsButton?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
});

const dialog = document.querySelector<HTMLElement>(".remote-connection-error-dialog");
ok(Boolean(dialog), "key-details action opens the security dialog");
ok(dialog?.textContent?.includes("SHA256:new") === true, "security dialog shows the presented fingerprint");
ok(dialog?.textContent?.includes("/home/dev/.ssh/known_hosts:7") === true, "security dialog shows the conflicting known_hosts record");

const closeButton = Array.from(dialog?.querySelectorAll("button") ?? []).find((button) => button.textContent?.includes("Close"));
await act(async () => {
  closeButton?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  root.render(
    <LocaleProvider>
      <StatusBar
        context={{ used: 0, window: 0, sessionTokens: 0 }}
        running={false}
        remoteHosts={[host]}
        remoteStatuses={{ box: degradedStatus }}
      />
    </LocaleProvider>,
  );
  useRemoteStore.getState().requestStatusPopover("box");
  await new Promise((resolve) => requestAnimationFrame(() => resolve(undefined)));
});

const warningCard = document.querySelector<HTMLElement>(".remote-switcher__error-card--warning");
ok(Boolean(warningCard), "degraded connection uses a warning card");
ok(warningCard?.textContent?.includes("SSH is connected") === true, "degraded warning explains that SSH remains connected");
ok(warningCard?.textContent?.includes("Connection failed") === false, "degraded warning does not claim the connection failed");

await act(async () => root.unmount());
dom.window.close();

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
