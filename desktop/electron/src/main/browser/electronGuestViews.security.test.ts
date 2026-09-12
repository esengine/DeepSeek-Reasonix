import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

test("guest browser views keep every node integration surface disabled", () => {
  const source = readFileSync(new URL("./electronGuestViews.ts", import.meta.url), "utf8");
  for (const setting of [
    "sandbox: true",
    "contextIsolation: true",
    "nodeIntegration: false",
    "nodeIntegrationInSubFrames: false",
    "nodeIntegrationInWorker: false",
  ]) {
    assert.match(source, new RegExp(setting.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")), setting);
  }
  assert.doesNotMatch(source, /nodeIntegration(?:InSubFrames|InWorker)?:\s*true/);
});
