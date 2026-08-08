import test from "node:test";
import assert from "node:assert/strict";
import {
  POC_CAPABILITIES,
  isCapabilityEnabled,
  unsupportedDesktopFeatures,
} from "../lib/capabilities.mjs";

test("PoC enables multi-tab/httpSse and disables terminal/remote/bot", () => {
  assert.equal(POC_CAPABILITIES.multiTab, true);
  assert.equal(POC_CAPABILITIES.singleSession, false);
  assert.equal(POC_CAPABILITIES.terminal, false);
  assert.equal(POC_CAPABILITIES.remote, false);
  assert.equal(POC_CAPABILITIES.bot, false);
  assert.equal(POC_CAPABILITIES.httpSse, true);
  assert.equal(isCapabilityEnabled(POC_CAPABILITIES, "toolApproval"), true);
  assert.equal(isCapabilityEnabled(POC_CAPABILITIES, "terminal"), false);
});

test("unsupportedDesktopFeatures lists deferred Wails-only surfaces", () => {
  const deferred = unsupportedDesktopFeatures();
  for (const f of ["terminal", "remote", "bot", "heartbeat", "steer"]) {
    assert.ok(deferred.includes(f), `expected ${f} deferred`);
  }
  assert.ok(!deferred.includes("multiTab"), "multiTab is enabled in PoC");
  assert.ok(!deferred.includes("httpSse"));
});
