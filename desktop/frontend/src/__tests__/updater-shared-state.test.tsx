// Run: tsx src/__tests__/updater-shared-state.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { __emitMockUpdater, type AppBindings } from "../lib/bridge";
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

function Consumer({ id, checking = false, channel = "" }: { id: string; checking?: boolean; channel?: string }) {
  const updater = useUpdater();
  return (
    <section>
      <output id={`${id}-status`}>{updater.status.kind}</output>
      <output id={`${id}-manual`}>
        {updater.status.kind === "error" && updater.status.manualHint ? "manual" : ""}
      </output>
      {checking && <button id={`${id}-check-update`} type="button" onClick={() => void updater.check(channel)}>Check</button>}
      {updater.status.kind === "available" && (
        <button id={`${id}-download`} type="button" onClick={() => updater.download(updater.status.info)}>Download</button>
      )}
      {updater.status.kind === "downloaded" && (
        <button id={`${id}-install`} type="button" onClick={updater.install}>Install</button>
      )}
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
      <Consumer id="settings" checking channel="preview" />
    </UpdaterProvider>,
  );
});

ok(document.getElementById("banner-status")?.textContent === "idle", "banner starts idle");
ok(document.getElementById("settings-status")?.textContent === "idle", "settings starts idle");

await act(async () => {
  (document.getElementById("banner-check-update") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});

ok(document.getElementById("banner-status")?.textContent === "upToDate", "banner receives the check result");
ok(document.getElementById("settings-status")?.textContent === "upToDate", "settings receives the same check result");

const debInfo = {
  available: true,
  current: "v1.0.0",
  latest: "v1.1.0",
  notes: "",
  channel: "stable",
  canSelfUpdate: true,
  manualOnly: false,
  installMode: "deb",
  requiresElevation: true,
  downloaded: false,
  downloadUrl: "https://example.invalid/download",
  assetSize: 42,
};
const installAttempts: Array<{ resolve: () => void; reject: (err: Error) => void }> = [];
const checkedChannels: string[] = [];
window.go = {
  main: {
    App: {
      async CheckUpdate(channel: string) {
        checkedChannels.push(channel);
        return debInfo;
      },
      async DownloadUpdate() {
        return { version: debInfo.latest, channel: debInfo.channel, path: "/tmp/update.deb", size: 42, sha256: "abc" };
      },
      InstallUpdate() {
        return new Promise<void>((resolve, reject) => installAttempts.push({ resolve, reject }));
      },
    } as AppBindings,
  },
};

await act(async () => {
  (document.getElementById("settings-check-update") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(checkedChannels[0] === "preview", "settings check forwards the selected Preview channel");
ok(document.getElementById("banner-status")?.textContent === "available", "deb update becomes available");
ok(document.getElementById("settings-status")?.textContent === "available", "deb availability is shared");

await act(async () => {
  (document.getElementById("banner-download") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(document.getElementById("banner-status")?.textContent === "downloaded", "deb download completes");

await act(async () => {
  (document.getElementById("banner-install") as HTMLButtonElement).click();
});
ok(document.getElementById("banner-status")?.textContent === "authorizing", "deb install starts authorizing");
ok(document.getElementById("settings-status")?.textContent === "authorizing", "authorizing state is shared");

await act(async () => {
  __emitMockUpdater({ phase: "installing", received: 42, total: 42 });
});
ok(document.getElementById("banner-status")?.textContent === "installing", "helper phase advances to installing");

await act(async () => {
  __emitMockUpdater({ phase: "downloaded", received: 42, total: 42 });
  installAttempts[0]?.resolve();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(document.getElementById("banner-status")?.textContent === "downloaded", "authorization cancellation returns to downloaded");

await act(async () => {
  (document.getElementById("banner-install") as HTMLButtonElement).click();
});
ok(document.getElementById("banner-status")?.textContent === "authorizing", "retry re-enters authorizing");

await act(async () => {
  installAttempts[1]?.reject(new Error("update: manual update required: system update helper is unavailable"));
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(document.getElementById("banner-status")?.textContent === "error", "manual reclassification leaves busy state");
ok(document.getElementById("banner-manual")?.textContent === "manual", "manual reclassification offers download fallback");
ok(document.getElementById("settings-status")?.textContent === "error", "manual fallback error is shared");

delete window.go;

await act(async () => root.unmount());

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
