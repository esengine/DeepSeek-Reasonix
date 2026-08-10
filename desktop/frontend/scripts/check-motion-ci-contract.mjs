#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const workflow = readFileSync(resolve(repoRoot, ".github/workflows/ci.yml"), "utf8");
const packageJSON = JSON.parse(readFileSync(resolve(repoRoot, "desktop/frontend/package.json"), "utf8"));
const appSource = readFileSync(resolve(repoRoot, "desktop/frontend/src/App.tsx"), "utf8");

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
  "Smoke-test Wails approval in WebView2",
  "../scripts/test-webview2-approval-smoke.ps1",
]) {
  if (!windowsJob.includes(required)) {
    throw new Error(`motion-ci-contract: desktop-windows must include ${required}`);
  }
}

if (!appSource.includes('lazy(() => import("./lib/useWebView2ApprovalSmoke")')) {
  throw new Error("motion-ci-contract: WebView2 smoke instrumentation must stay out of the normal startup bundle");
}
if (appSource.includes('from "./lib/useWebView2ApprovalSmoke"')) {
  throw new Error("motion-ci-contract: WebView2 smoke instrumentation must not use a static App import");
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

const transcriptScript = packageJSON.scripts?.["test:transcript"] ?? "";
if (transcriptScript !== "tsx src/__tests__/transcript-selection-runtime.test.ts && tsx src/__tests__/scroll-manager.test.tsx && tsx src/__tests__/transcript-selection-retention.test.tsx && tsx src/__tests__/transcript-virtualization.test.tsx") {
  throw new Error("motion-ci-contract: test:transcript must own the transcript selection and virtualization suites");
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

console.log("motion-ci-contract: required jobs run focused native motion gates, Linux owns transcript virtualization, and Windows runs the real WebView2 smoke");
