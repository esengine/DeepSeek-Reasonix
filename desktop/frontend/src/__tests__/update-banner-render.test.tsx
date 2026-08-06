// Run: node --import tsx src/__tests__/update-banner-render.test.tsx
//
// UpdateBanner rendering: every status.kind the banner can show (available /
// downloading / verifying / authorizing / installing / relaunching / done /
// error), the null states (idle / checking / upToDate), per-version dismissal,
// download MB/percent formatting, the three error dispositions (retryable /
// recovery / manual), and the macOS manual-download hint.
//
// useUpdater is mocked the same way updater-shared-state.test.tsx does it: the
// real hook runs inside UpdaterProvider but its window.go bridge bindings are
// replaced, and progress phases are pushed through __emitMockUpdater.

import { JSDOM } from "jsdom";
import React from "react";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { __emitMockUpdater } from "../lib/bridge";
import { UpdaterProvider, useUpdater } from "../lib/useUpdater";
import { LocaleProvider } from "../lib/i18n";
import { UpdateBanner } from "../components/UpdateBanner";
import type { AppBindings } from "../lib/bridge";
import type { UpdateInfo } from "../lib/types";

const MB = 1024 * 1024;

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

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function installDom() {
  const dom = new JSDOM("<!doctype html><html><head></head><body><div id=\"root\"></div></body></html>", {
    pretendToBeVisual: true,
    url: "http://localhost/",
  });
  (globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  Object.defineProperty(dom.window.navigator, "language", { configurable: true, value: "en-US" });
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Event = dom.window.Event;
  globalThis.KeyboardEvent = dom.window.KeyboardEvent;
  globalThis.MouseEvent = dom.window.MouseEvent;
  return dom;
}

function flush(ms = 0): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

const banner = () => document.querySelector(".banner--update");
// Simple states (verifying/authorizing/installing/relaunching/done) render the
// message as bare text; available/downloading/error use a .banner__msg span.
const bannerText = () => document.querySelector(".banner--update")?.textContent ?? null;
const bannerMsg = () => document.querySelector<HTMLElement>(".banner--update .banner__msg")?.textContent ?? null;
const bannerHint = () => document.querySelector<HTMLElement>(".banner--update .banner__hint")?.textContent ?? null;
const bannerButton = (label: string) =>
  [...document.querySelectorAll<HTMLButtonElement>(".banner--update button")].find((b) => b.textContent === label) ?? null;

// Harness exposes check/reset so the shared updater context can be driven
// without re-mounting the banner (mirrors updater-shared-state.test.tsx).
function Harness({
  enabled = true,
  onShowReleaseNotes,
}: {
  enabled?: boolean;
  onShowReleaseNotes?: (version: string) => void;
}) {
  const updater = useUpdater();
  return (
    <>
      <UpdateBanner enabled={enabled} onShowReleaseNotes={onShowReleaseNotes} />
      <button id="harness-check" type="button" onClick={() => void updater.check()}>Check</button>
      <button id="harness-reset" type="button" onClick={() => updater.reset()}>Reset</button>
    </>
  );
}

function makeInfo(over: Partial<UpdateInfo> & { latest: string }): UpdateInfo {
  return {
    available: true,
    current: "v1.1.0",
    latest: over.latest,
    notes: "",
    channel: "stable",
    canSelfUpdate: true,
    downloaded: false,
    downloadUrl: "https://example.invalid/download",
    assetSize: 10 * MB,
    ...over,
  };
}

console.log("\nupdate banner");

const dom = installDom();
const root = createRoot(document.getElementById("root")!);

// Mock bridge bindings. `app` in bridge.ts proxies to window.go.main.App on
// every call, so swapping these mid-test drives the real useUpdater machine.
let checkImpl: (channel: string) => Promise<UpdateInfo | null> = () => new Promise(() => {});
let applyImpl: () => Promise<void> = async () => {};
let openDownloadCalls = 0;
let checkCalls = 0;
let applyAttempts: Array<{ channel: string; version: string; requestId: string }> = [];
let resolveFirstCheck!: (info: UpdateInfo | null) => void;
checkImpl = () => new Promise((resolve) => {
  resolveFirstCheck = resolve;
});

window.go = {
  main: {
    App: {
      async CheckUpdate(channel: string) {
        checkCalls += 1;
        return checkImpl(channel);
      },
      async ApplyUpdateRequest(channel: string, expectedVersion: string, requestId: string) {
        applyAttempts.push({ channel, version: expectedVersion, requestId });
        return applyImpl();
      },
      async OpenDownloadPage() {
        openDownloadCalls += 1;
      },
    } as AppBindings,
  },
};

const renderHarness = (props: { enabled?: boolean; onShowReleaseNotes?: (version: string) => void } = {}) =>
  act(async () => {
    root.render(
      <LocaleProvider>
        <UpdaterProvider>
          <Harness enabled={props.enabled} onShowReleaseNotes={props.onShowReleaseNotes} />
        </UpdaterProvider>
      </LocaleProvider>,
    );
    await flush();
  });

// -- disabled: nothing renders and no check fires
await renderHarness({ enabled: false });
ok(!banner(), "disabled banner renders nothing");
eq(checkCalls, 0, "disabled banner never triggers a check");

// -- mount check: checking state renders nothing until it resolves
await renderHarness();
eq(checkCalls, 1, "enabled banner checks once on mount");
ok(!banner(), "checking state renders nothing");

// -- available
const v120 = makeInfo({ latest: "v1.2.0", installMode: "portable", requiresElevation: false });
await act(async () => {
  resolveFirstCheck(v120);
  await flush();
});
ok(!!banner(), "available state shows the banner");
eq(bannerMsg(), "A new version is available: v1.2.0", "available banner names the version");
ok(!bannerHint(), "self-updatable builds show no manual hint");
ok(bannerButton("Update and restart")?.className.includes("btn--primary") === true, "self-update action is the primary button");
ok(!!bannerButton("Later"), "available banner offers dismissal");
ok(!bannerButton("Release notes"), "no release-notes button without a callback");

// -- release notes callback appears and reports the version
const releaseNotesVersions: string[] = [];
await renderHarness({ onShowReleaseNotes: (v) => releaseNotesVersions.push(v) });
eq(checkCalls, 1, "re-rendering with a callback does not re-check");
ok(!!bannerButton("Release notes"), "release-notes button appears when a callback is provided");
await act(async () => {
  bannerButton("Release notes")!.click();
});
eq(releaseNotesVersions[0], "v1.2.0", "release-notes button reports the available version");

// -- apply → downloading: MB/percent formatting and progress bar
await act(async () => {
  bannerButton("Update and restart")!.click();
});
eq(applyAttempts.length, 1, "update action starts a single apply");
eq(bannerMsg(), "Downloading… 0.0 MB / 10.0 MB (0%)", "download starts at 0% with MB formatting");
eq(document.querySelector(".banner--update progress")?.getAttribute("value"), "0", "progress bar starts at zero");
eq(document.querySelector(".banner--update progress")?.getAttribute("max"), String(10 * MB), "progress bar exposes the total size");

await act(async () => {
  __emitMockUpdater({
    requestId: applyAttempts[0].requestId,
    version: applyAttempts[0].version,
    channel: "stable",
    phase: "downloading",
    received: 0,
    total: 0,
  });
});
eq(bannerMsg(), "Downloading… 0.0 MB / 0.0 MB (0%)", "unknown total formats as 0.0 MB with 0%");
eq(document.querySelector(".banner--update progress")?.getAttribute("max"), null, "unknown total leaves progress max unset");

await act(async () => {
  __emitMockUpdater({
    requestId: applyAttempts[0].requestId,
    version: applyAttempts[0].version,
    channel: "stable",
    phase: "downloading",
    received: 2.5 * MB,
    total: 10 * MB,
  });
});
eq(bannerMsg(), "Downloading… 2.5 MB / 10.0 MB (25%)", "download progress formats MB and percent");
eq(document.querySelector(".banner--update progress")?.getAttribute("value"), String(2.5 * MB), "progress bar tracks received bytes");

// -- verifying / authorizing / installing / relaunching
const progress = {
  requestId: applyAttempts[0].requestId,
  version: applyAttempts[0].version,
  channel: "stable",
  received: 2.5 * MB,
  total: 10 * MB,
} as const;
await act(async () => {
  __emitMockUpdater({ ...progress, phase: "verifying" });
});
eq(bannerText(), "Verifying signature…", "verifying state renders the verify message");
await act(async () => {
  __emitMockUpdater({ ...progress, phase: "authorizing" });
});
eq(bannerText(), "Waiting for administrator authorization…", "authorizing state renders the authorization message");
await act(async () => {
  __emitMockUpdater({ ...progress, phase: "installing" });
});
eq(bannerText(), "Installing — Reasonix will restart…", "portable install uses the generic installing message");
await act(async () => {
  __emitMockUpdater({ ...progress, phase: "relaunching" });
});
eq(bannerText(), "Update complete — restarting…", "relaunching state renders the done message");

// -- error disposition: retryable (no official download; retry is primary)
await act(async () => {
  __emitMockUpdater({ ...progress, phase: "error", err: "connection reset by peer" });
});
eq(bannerMsg(), "Update failed: connection reset by peer", "retryable errors show the failure message");
eq(
  document.querySelector<HTMLElement>(".banner--update .banner__msg")?.getAttribute("title"),
  "Update failed: connection reset by peer",
  "error message is repeated in the title tooltip",
);
ok(!!document.querySelector(".banner--error.banner--actionable"), "error banner carries error/actionable styling");
ok(!bannerButton("Download from official site"), "retryable errors skip the official download action");
ok(bannerButton("Retry")?.className.includes("btn--primary") === true, "retry is the primary action for retryable errors");
ok(!!bannerButton("Later"), "error banner can be dismissed");
await act(async () => {
  bannerButton("Retry")!.click();
});
eq(applyAttempts.length, 2, "retry with update info resumes applying");
eq(bannerMsg(), "Downloading… 0.0 MB / 10.0 MB (0%)", "retry restarts the download");

// -- error disposition: recovery (official download offered)
await act(async () => {
  __emitMockUpdater({
    requestId: applyAttempts[1].requestId,
    version: applyAttempts[1].version,
    channel: "stable",
    phase: "error",
    received: 0,
    total: 0,
    err: "prepare update: a pending update already exists",
  });
});
eq(bannerMsg(), "The previous update has not finished.", "recovery errors show the recovery message");
ok(!!bannerButton("Download from official site"), "recovery errors offer the official download");
await act(async () => {
  bannerButton("Download from official site")!.click();
});
eq(openDownloadCalls, 1, "official download opens the download page");
await act(async () => {
  bannerButton("Retry")!.click();
});
eq(applyAttempts.length, 3, "recovery errors still allow retry");
eq(bannerMsg(), "Downloading… 0.0 MB / 10.0 MB (0%)", "recovery retry restarts the download");

// -- error disposition: manual (official download offered)
await act(async () => {
  __emitMockUpdater({
    requestId: applyAttempts[2].requestId,
    version: applyAttempts[2].version,
    channel: "stable",
    phase: "error",
    received: 0,
    total: 0,
    err: "update: manual update required: system update helper is unavailable",
  });
});
eq(bannerMsg(), "Automatic installation is unavailable.", "manual errors show the manual fallback message");
ok(!!bannerButton("Download from official site"), "manual errors offer the official download");
await act(async () => {
  bannerButton("Retry")!.click();
});
eq(applyAttempts.length, 4, "manual errors still allow retry");

// -- error without info: retry falls back to a fresh check
await act(async () => {
  (document.getElementById("harness-reset") as HTMLButtonElement).click();
  await flush();
});
ok(!banner(), "reset returns to idle and hides the banner");
checkImpl = async () => {
  throw new Error("network down");
};
await act(async () => {
  (document.getElementById("harness-check") as HTMLButtonElement).click();
  await flush();
});
eq(bannerMsg(), "Update failed: network down", "a failed check shows the error message");
ok(!!bannerButton("Retry"), "error state always offers retry");
ok(!bannerButton("Download from official site"), "retryable check errors do not show the official download button");
const checksBeforeRetry = checkCalls;
checkImpl = async () => null;
await act(async () => {
  bannerButton("Retry")!.click();
  await flush();
});
eq(checkCalls, checksBeforeRetry + 1, "retry without update info triggers a fresh check");
ok(!banner(), "up-to-date result after retry hides the banner");

// -- macOS manual build: hint + go-to-download action
checkImpl = async () => makeInfo({ latest: "v1.3.0", canSelfUpdate: false, installMode: "manual" });
await act(async () => {
  (document.getElementById("harness-check") as HTMLButtonElement).click();
  await flush();
});
ok(!!banner(), "re-check surfaces the new version");
eq(bannerMsg(), "A new version is available: v1.3.0", "manual-build banner names the version");
eq(
  bannerHint(),
  "Automatic install isn't available for this macOS build — please download and install manually.",
  "mac manual builds show the download hint",
);
ok(!!bannerButton("Go to download page"), "mac manual builds offer the download action");
ok(!bannerButton("Update and restart"), "mac manual builds do not offer self-update");
await act(async () => {
  bannerButton("Go to download page")!.click();
});
eq(openDownloadCalls, 2, "download action opens the download page");
ok(!!banner(), "download action keeps the banner available");

checkImpl = async () => makeInfo({ latest: "v1.3.0", canSelfUpdate: false, installMode: "manual", manualReason: "Your admin requires a signed installer." });
await act(async () => {
  (document.getElementById("harness-reset") as HTMLButtonElement).click();
  (document.getElementById("harness-check") as HTMLButtonElement).click();
  await flush();
});
eq(bannerHint(), "Your admin requires a signed installer.", "a custom manual reason overrides the mac hint");

// -- dismissal is per-version
await act(async () => {
  bannerButton("Later")!.click();
});
ok(!banner(), "dismiss hides the available banner");
checkImpl = async () => makeInfo({ latest: "v1.4.0", canSelfUpdate: false, installMode: "manual" });
await act(async () => {
  (document.getElementById("harness-check") as HTMLButtonElement).click();
  await flush();
});
ok(!!banner(), "a newer version surfaces again after dismissal");
eq(bannerMsg(), "A new version is available: v1.4.0", "newer version banner names the newer version");
await act(async () => {
  bannerButton("Later")!.click();
});
ok(!banner(), "dismissing the newer version hides it");
checkImpl = async () => makeInfo({ latest: "v1.3.0", canSelfUpdate: false, installMode: "manual" });
await act(async () => {
  (document.getElementById("harness-check") as HTMLButtonElement).click();
  await flush();
});
ok(!!banner(), "an older dismissed version can return after a newer dismissal");

// -- deb/elevated install: authorizing first, package-manager install, done
checkImpl = async () => makeInfo({ latest: "v1.5.0", installMode: "deb", requiresElevation: true });
await act(async () => {
  (document.getElementById("harness-check") as HTMLButtonElement).click();
  await flush();
});
eq(bannerMsg(), "A new version is available: v1.5.0", "deb banner names the version");
await act(async () => {
  bannerButton("Update and restart")!.click();
});
eq(bannerText(), "Waiting for administrator authorization…", "elevated installs authorize before downloading");
const debApply = applyAttempts[applyAttempts.length - 1];
await act(async () => {
  __emitMockUpdater({
    requestId: debApply.requestId,
    version: debApply.version,
    channel: "stable",
    phase: "installing",
    received: 10 * MB,
    total: 10 * MB,
  });
});
eq(bannerText(), "Installing via the system package manager…", "deb/elevated installs use the package-manager message");
await act(async () => {
  __emitMockUpdater({
    requestId: debApply.requestId,
    version: debApply.version,
    channel: "stable",
    phase: "done",
    received: 10 * MB,
    total: 10 * MB,
  });
});
eq(bannerText(), "Update complete — restarting…", "done state renders the done message");

// -- disabling mid-flight hides the banner without another check
const checksBeforeDisable = checkCalls;
await renderHarness({ enabled: false });
ok(!banner(), "disabling the banner hides it even in a visible state");
eq(checkCalls, checksBeforeDisable, "disabling the banner does not trigger another check");

await act(async () => {
  root.unmount();
});
dom.window.close();

process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
