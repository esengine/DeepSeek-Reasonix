// Run: tsx src/__tests__/recovery-quiet-notifications.test.ts
//
// Recovery copies remain protected by the backend, but the desktop UI should
// not interrupt users with recovery lifecycle banners, toasts, or transcript
// notices.

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
const controllerSource = readFileSync(resolve(here, "../lib/useController.ts"), "utf8");

console.log("\nquiet recovery notifications");

ok(!appSource.includes("banner--recovery"), "App does not render a recovery banner");
ok(!appSource.includes("recovery.toast"), "App does not show a recovery-created toast");
ok(!appSource.includes("onSessionRecoveryFailed"), "App does not show recovery failure toasts");
ok(!appSource.includes("AcknowledgeTabRecovery"), "App does not expose recovery acknowledgement controls");
ok(!appSource.includes("OpenTabRecoveryParent"), "App does not expose recovery compare controls");
ok(!appSource.includes("recovery.openOriginalFailed"), "App does not carry recovery compare failure text");

ok(controllerSource.includes("function quietTranscriptNoticeKey"), "controller centralizes quiet transcript notices");
ok(controllerSource.includes("if (quietTranscriptNoticeKey(rawText))"), "raw quiet notices are skipped before localization");
ok(controllerSource.includes("if (quietTranscriptNoticeKey(text))"), "localized quiet notices are skipped before rendering");

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
