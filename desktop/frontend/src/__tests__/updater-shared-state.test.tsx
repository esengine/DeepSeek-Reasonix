// Run: tsx src/__tests__/updater-shared-state.test.tsx

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { __emitMockUpdater, type AppBindings } from "../lib/bridge";
import { switchUpdaterChannel, UpdaterProvider, useUpdater } from "../lib/useUpdater";

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
      <output id={`${id}-received`}>
        {updater.status.kind === "downloading" ? updater.status.received : ""}
      </output>
      {checking && <button id={`${id}-check-update`} type="button" onClick={() => void updater.check(channel)}>Check</button>}
      <button id={`${id}-reset-stable`} type="button" onClick={() => updater.reset("stable")}>Reset stable</button>
      <button id={`${id}-reset-preview`} type="button" onClick={() => updater.reset("preview")}>Reset preview</button>
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
const installAttempts: Array<{
  requestId: string;
  version: string;
  channel: string;
  resolve: () => void;
  reject: (err: Error) => void;
}> = [];
const checkedChannels: string[] = [];
window.go = {
  main: {
    App: {
      async CheckUpdate(channel: string) {
        checkedChannels.push(channel);
        return { ...debInfo, channel: channel === "preview" ? "preview" : "stable" };
      },
      async DownloadUpdateRequest(channel: string, expectedVersion: string, requestId: string) {
        return {
          requestId,
          version: expectedVersion,
          channel: channel === "preview" ? "preview" : "stable",
          path: "/tmp/update.deb",
          size: 42,
          sha256: "abc",
        };
      },
      InstallUpdateRequest(channel: string, expectedVersion: string, requestId: string) {
        return new Promise<void>((resolve, reject) => installAttempts.push({
          requestId,
          version: expectedVersion,
          channel,
          resolve,
          reject,
        }));
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
  __emitMockUpdater({
    requestId: installAttempts[0].requestId,
    version: installAttempts[0].version,
    channel: "preview",
    phase: "installing",
    received: 42,
    total: 42,
  });
});
ok(document.getElementById("banner-status")?.textContent === "installing", "helper phase advances to installing");

await act(async () => {
  __emitMockUpdater({
    requestId: installAttempts[0].requestId,
    version: installAttempts[0].version,
    channel: "preview",
    phase: "downloaded",
    received: 42,
    total: 42,
  });
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

let resolveSavedCheck!: (value: typeof debInfo) => void;
let resolvePreviewCheck!: (value: typeof debInfo) => void;
window.go.main.App.CheckUpdate = (selected: string) =>
  new Promise<typeof debInfo>((resolve) => {
    if (selected === "preview") resolvePreviewCheck = resolve;
    else resolveSavedCheck = resolve;
  });

await act(async () => {
  (document.getElementById("banner-check-update") as HTMLButtonElement).click();
  (document.getElementById("settings-check-update") as HTMLButtonElement).click();
});
await act(async () => {
  resolvePreviewCheck({ ...debInfo, channel: "preview", latest: "v1.2.0" });
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(document.getElementById("banner-status")?.textContent === "available", "newest channel check publishes its result");
await act(async () => {
  resolveSavedCheck({ ...debInfo, available: false, channel: "stable", latest: "v1.0.0" });
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(document.getElementById("banner-status")?.textContent === "available", "stale channel check cannot overwrite the newest result");

let oldDownloadRequestId = "";
let oldDownloadVersion = "";
let resolveOldDownload!: (value: { requestId: string; version: string; channel: string; path: string; size: number; sha256: string }) => void;
window.go.main.App.DownloadUpdateRequest = (_channel: string, expectedVersion: string, requestId: string) =>
  new Promise((resolve) => {
    oldDownloadRequestId = requestId;
    oldDownloadVersion = expectedVersion;
    resolveOldDownload = resolve;
  });
await act(async () => {
  (document.getElementById("banner-download") as HTMLButtonElement).click();
});
ok(document.getElementById("banner-status")?.textContent === "downloading", "Preview download starts");
await act(async () => {
  __emitMockUpdater({
    requestId: oldDownloadRequestId,
    version: `${oldDownloadVersion}-wrong`,
    channel: "preview",
    phase: "downloaded",
    received: 42,
    total: 42,
  });
});
ok(
  document.getElementById("banner-status")?.textContent === "downloading",
  "same-request wrong-version progress cannot complete the active Preview download",
);
await act(async () => {
  __emitMockUpdater({
    requestId: oldDownloadRequestId,
    version: oldDownloadVersion,
    channel: "stable",
    phase: "downloading",
    received: 41,
    total: 42,
  });
});
ok(document.getElementById("banner-received")?.textContent === "0", "wrong-channel progress cannot update the active Preview download");
await act(async () => {
  (document.getElementById("banner-reset-preview") as HTMLButtonElement).click();
  __emitMockUpdater({
    requestId: oldDownloadRequestId,
    version: oldDownloadVersion,
    channel: "preview",
    phase: "downloaded",
    received: 42,
    total: 42,
  });
  resolveOldDownload({
    requestId: oldDownloadRequestId,
    version: oldDownloadVersion,
    channel: "preview",
    path: "/tmp/old.deb",
    size: 42,
    sha256: "abc",
  });
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(document.getElementById("banner-status")?.textContent === "idle", "same-channel superseded progress and Promise completion stay ignored");

window.go.main.App.CheckUpdate = async (selected: string) => ({
  ...debInfo,
  channel: selected === "preview" ? "preview" : "stable",
  latest: selected === "preview" ? "v1.2.0" : "v1.1.0",
});
window.go.main.App.DownloadUpdateRequest = async (selected: string, _expectedVersion: string, requestId: string) => ({
  requestId,
  version: "v1.2.0-preview.99",
  channel: selected === "preview" ? "preview" : "stable",
  path: "/tmp/new.deb",
  size: 42,
  sha256: "abc",
});
await act(async () => {
  (document.getElementById("settings-check-update") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
await act(async () => {
  (document.getElementById("banner-download") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(document.getElementById("banner-status")?.textContent === "available", "download result for a different version is rejected");

window.go.main.App.DownloadUpdateRequest = async (selected: string, expectedVersion: string, requestId: string) => ({
  requestId,
  version: expectedVersion,
  channel: selected === "preview" ? "preview" : "stable",
  path: "/tmp/new.deb",
  size: 42,
  sha256: "abc",
});
await act(async () => {
  (document.getElementById("banner-download") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(document.getElementById("banner-status")?.textContent === "downloaded", "Preview update is ready for install");
await act(async () => {
  (document.getElementById("banner-install") as HTMLButtonElement).click();
});
const staleInstall = installAttempts[installAttempts.length - 1];
ok(document.getElementById("banner-status")?.textContent === "authorizing", "Preview install starts");
await act(async () => {
  (document.getElementById("banner-reset-stable") as HTMLButtonElement).click();
  __emitMockUpdater({
    requestId: staleInstall.requestId,
    version: staleInstall.version,
    channel: "preview",
    phase: "error",
    received: 0,
    total: 0,
    err: "old install failed",
  });
  staleInstall.reject(new Error("old Preview install failed"));
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(document.getElementById("banner-status")?.textContent === "idle", "superseded install progress and rejection stay ignored");

window.go.main.App.CheckUpdate = async () => ({ ...debInfo, channel: "stable" });
await act(async () => {
  (document.getElementById("settings-check-update") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(document.getElementById("banner-status")?.textContent === "error", "wrong-channel check response leaves the checking state");

window.go.main.App.CheckUpdate = async () => ({ ...debInfo, channel: "preview", latest: "v1.3.0" });
await act(async () => {
  (document.getElementById("settings-check-update") as HTMLButtonElement).click();
  await new Promise((resolve) => setTimeout(resolve, 0));
});
ok(document.getElementById("banner-status")?.textContent === "available", "check can retry after a wrong-channel response");

delete window.go;

const channelTransitions: string[] = [];
let releaseSave!: (saved: boolean) => void;
const pendingSwitch = switchUpdaterChannel(
  "preview",
  (channel) => {
    channelTransitions.push(`invalidate:${channel}`);
  },
  (channel) =>
    new Promise<boolean>((resolve) => {
      channelTransitions.push(`save:${channel}`);
      releaseSave = resolve;
    }),
  async (channel) => {
    channelTransitions.push(`check:${channel}`);
  },
);
ok(channelTransitions.join(",") === "invalidate:preview,save:preview", "channel switch invalidates old updater state before persistence resolves");
releaseSave(true);
await pendingSwitch;
ok(channelTransitions.join(",") === "invalidate:preview,save:preview,check:preview", "successful channel persistence checks the target pointer");

await switchUpdaterChannel(
  "stable",
  (channel) => {
    channelTransitions.push(`invalidate:${channel}`);
  },
  async (channel) => {
    channelTransitions.push(`failed:${channel}`);
    return false;
  },
  async (channel) => {
    channelTransitions.push(`unexpected:${channel}`);
  },
);
ok(!channelTransitions.includes("unexpected:stable"), "failed channel persistence does not check an unselected channel");

await act(async () => root.unmount());

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
