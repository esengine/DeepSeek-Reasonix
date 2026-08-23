import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { heartbeatFeatureKeys } from "../custom/features/heartbeat/heartbeat.i18n";

const testDir = dirname(fileURLToPath(import.meta.url));
const globalLocales = ["en.ts", "zh.ts", "zh-TW.ts"].map((name) =>
  readFileSync(resolve(testDir, "../locales", name), "utf8"),
);

assert.ok(heartbeatFeatureKeys.length > 0, "Heartbeat owns feature-local translations");
for (const key of heartbeatFeatureKeys) {
  for (const source of globalLocales) {
    assert.ok(!source.includes(`"${key}"`), `${key} stays out of the global locale chunks`);
  }
}

const panel = readFileSync(resolve(testDir, "../custom/features/heartbeat/HeartbeatPanel.tsx"), "utf8");
assert.match(panel, /const t = useHeartbeatT\(\)/, "the lazy Heartbeat page resolves its feature-local translations");

console.log(`  PASS  ${heartbeatFeatureKeys.length} Heartbeat translations stay feature-local`);
