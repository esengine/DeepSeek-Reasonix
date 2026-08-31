// Run: tsx src/__tests__/recovery-quiet-notifications.test.ts
//
// Recovery copies remain protected by the backend. Covered/adopted lifecycle
// noise stays quiet; only catalog-confirmed unique divergence gets one toast.

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { en } from "../locales/en";
import { zh } from "../locales/zh";
import { zhTW } from "../locales/zh-TW";

let passed = 0;
let failed = 0;

function ok(cond: boolean, label: string) {
  if (cond) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

const here = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(here, "../App.tsx"), "utf8");
const recoveryHostSource = readFileSync(resolve(here, "../components/SessionRecoveryVersionsHost.tsx"), "utf8");
const recoveryRuntimeSource = readFileSync(resolve(here, "../lib/sessionRecoveryRuntime.ts"), "utf8");
const controllerSource = readFileSync(resolve(here, "../lib/useController.ts"), "utf8");
const noticesSource = readFileSync(resolve(here, "../lib/controllerNotices.ts"), "utf8");

console.log("\nquiet recovery notifications and confirmed divergence");

ok(!appSource.includes("banner--recovery"), "App does not render a recovery banner");
ok(!appSource.includes('t("recovery.noticeSavedCopy")'), "App does not toast when a physical recovery copy is merely created");
ok(recoveryHostSource.includes("recovery.divergedToast"), "App has a dedicated confirmed-divergence toast");
ok(recoveryRuntimeSource.includes("onProjectTreeChangedV2"), "divergence is rechecked after catalog revisions");
ok(!appSource.includes("onSessionRecoveryFailed"), "App does not show recovery failure toasts");
ok(!appSource.includes("AcknowledgeTabRecovery"), "App does not expose recovery acknowledgement controls");
ok(!appSource.includes("OpenTabRecoveryParent"), "App does not expose recovery compare controls");
ok(!appSource.includes("recovery.openOriginalFailed"), "App does not carry recovery compare failure text");

ok(noticesSource.includes("function quietTranscriptNoticeKey"), "controller centralizes quiet transcript notices");
ok(controllerSource.includes("if (quietTranscriptNoticeKey(rawText, code))"), "raw quiet notices are skipped before localization");
ok(controllerSource.includes("if (quietTranscriptNoticeKey(text, code))"), "localized quiet notices are skipped before rendering");

const removedPromptKeys = [
  "recovery.open",
  "recovery.toast",
  "recovery.failedLease",
  "recovery.failedUnavailable",
  "recovery.banner",
  "recovery.bannerCompare",
  "recovery.bannerDismiss",
  "recovery.openOriginalFailed",
];
for (const [name, dict] of [["en", en], ["zh", zh], ["zh-TW", zhTW]] as const) {
  for (const key of removedPromptKeys) {
    ok(!(key in dict), `${name} locale omits ${key}`);
  }
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
