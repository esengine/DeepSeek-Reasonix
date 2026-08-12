#!/usr/bin/env node

import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const workflow = readFileSync(resolve(repoRoot, ".github/workflows/ci.yml"), "utf8");
const packageJSON = JSON.parse(readFileSync(resolve(repoRoot, "desktop/frontend/package.json"), "utf8"));
const appSource = readFileSync(resolve(repoRoot, "desktop/frontend/src/App.tsx"), "utf8");
const bridgeSource = readFileSync(resolve(repoRoot, "desktop/frontend/src/lib/bridge.ts"), "utf8");
const desktopMainSource = readFileSync(resolve(repoRoot, "desktop/main.go"), "utf8");

function jobBody(name, nextName) {
  const match = workflow.match(new RegExp(`\\n  ${name}:\\n([\\s\\S]*?)\\n  ${nextName}:`));
  if (!match) throw new Error(`motion-ci-contract: could not locate ${name} job`);
  return match[1];
}

for (const [job, body, command] of [
  ["desktop", jobBody("desktop", "desktop-macos"), "pnpm --dir frontend test:motion"],
  ["desktop-windows", jobBody("desktop-windows", "lint"), "pnpm --dir frontend test:motion"],
  ["required lint", jobBody("lint", "site"), "pnpm --dir desktop/frontend test:motion"],
]) {
  if (!body.includes(command)) {
    throw new Error(`motion-ci-contract: ${job} must run test:motion`);
  }
}

const windowsJob = jobBody("desktop-windows", "lint");
for (const required of [
  "wails build -clean -s -skipbindings -nopackage -platform windows/amd64 -webview2 embed",
  "Test WebView2 native smoke state machine",
  "../scripts/test-webview2-native-smoke.ps1 -SelfTest",
  "Smoke-test Wails/WebView2 native startup",
  "../scripts/test-webview2-native-smoke.ps1",
]) {
  if (!windowsJob.includes(required)) {
    throw new Error(`motion-ci-contract: desktop-windows must include ${required}`);
  }
}

for (const [path, source] of [
  ["desktop/main.go", desktopMainSource],
  ["desktop/frontend/src/App.tsx", appSource],
  ["desktop/frontend/src/lib/bridge.ts", bridgeSource],
]) {
  for (const forbidden of [
    "REASONIX_WEBVIEW2_APPROVAL_SMOKE",
    "__REASONIX_WEBVIEW2_APPROVAL_SMOKE__",
    "WebView2ApprovalSmokeBridge",
  ]) {
    if (source.includes(forbidden)) {
      throw new Error(`motion-ci-contract: ${path} must not embed test-only WebView2 instrumentation (${forbidden})`);
    }
  }
}
for (const retiredPath of [
  "desktop/webview2_approval_smoke.go",
  "desktop/frontend/src/lib/useWebView2ApprovalSmoke.ts",
  "desktop/frontend/src/lib/webView2ApprovalSmoke.ts",
  "scripts/test-webview2-approval-smoke.ps1",
]) {
  if (existsSync(resolve(repoRoot, retiredPath))) {
    throw new Error(`motion-ci-contract: retired production smoke path still exists: ${retiredPath}`);
  }
}

const motionScript = packageJSON.scripts?.["test:motion"] ?? "";
for (const required of [
  "check-waapi-contract.mjs --self-test",
  "native-motion.test.tsx",
  "approval-animation.test.tsx",
]) {
  if (!motionScript.includes(required)) {
    throw new Error(`motion-ci-contract: test:motion must include ${required}`);
  }
}

if (motionScript.includes("transcript-virtualization.test.tsx")) {
  throw new Error("motion-ci-contract: test:motion must not include the transcript virtualization suite");
}

const motionBrowserCommand = "pnpm --dir frontend test:motion-browser";
const motionBrowserRuns = workflow.match(/pnpm --dir frontend test:motion-browser(?:\s|$)/g)?.length ?? 0;
if (!jobBody("desktop", "desktop-macos").includes(motionBrowserCommand) || motionBrowserRuns !== 1) {
  throw new Error("motion-ci-contract: the Linux desktop job must run test:motion-browser exactly once");
}
if (!packageJSON.scripts?.["test:motion-browser"]?.includes("approval-animation.mjs")) {
  throw new Error("motion-ci-contract: test:motion-browser must exercise the approval animation in real Chromium");
}

const transcriptScript = packageJSON.scripts?.["test:transcript"] ?? "";
for (const required of [
  "transcript-scroll-session.test.ts",
  "nested-scroll-handoff.test.ts",
  "creation-transcript-scrollbar.test.ts",
  "transcript-measurement-invalidation.test.tsx",
  "markdown-table-virtual.test.tsx",
  "typography-overflow-contract.test.ts",
  "transcript-selection-runtime.test.ts",
  "scroll-manager.test.tsx",
  "transcript-selection-retention.test.tsx",
  "transcript-logical-selection.test.ts",
  "markdown-pipeline.test.tsx",
  "message-selection-copy.test.ts",
  "transcript-selection-menu.test.tsx",
  "transcript-store.test.ts",
  "transcript-virtualization.test.tsx",
]) {
  if (!transcriptScript.includes(required)) {
    throw new Error(`motion-ci-contract: test:transcript must include ${required}`);
  }
}

const transcriptCommand = "pnpm --dir frontend test:transcript";
const transcriptRuns = workflow.match(/pnpm --dir frontend test:transcript(?:\s|$)/g)?.length ?? 0;
if (!jobBody("desktop", "desktop-macos").includes(transcriptCommand) || transcriptRuns !== 1) {
  throw new Error("motion-ci-contract: the Linux desktop job must run test:transcript exactly once");
}

const transcriptBrowserCommand = "pnpm --dir frontend test:transcript-browser";
const transcriptBrowserRuns = workflow.match(/pnpm --dir frontend test:transcript-browser(?:\s|$)/g)?.length ?? 0;
if (!jobBody("desktop", "desktop-macos").includes(transcriptBrowserCommand) || transcriptBrowserRuns !== 1) {
  throw new Error("motion-ci-contract: the Linux desktop job must run test:transcript-browser exactly once");
}
if (!jobBody("desktop", "desktop-macos").includes("PLAYWRIGHT_BROWSERS_PATH=.pw-browsers pnpm --dir frontend exec playwright install")) {
  throw new Error("motion-ci-contract: Chromium must install into the path used by frontend browser tests");
}
for (const required of ["transcript-selection.mjs", "transcript-scroll-stability.mjs"]) {
  if (!packageJSON.scripts?.["test:transcript-browser"]?.includes(required)) {
    throw new Error(`motion-ci-contract: test:transcript-browser must include ${required}`);
  }
}

console.log("motion-ci-contract: browser approval behavior and exact-binary WebView2 startup are separate release gates without production smoke instrumentation");
