import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { createAppRenderToken, commitAppRenderToken, trackAppOperation, trackAppSubscription } from "../app-runtime/appLifecycleProbe";

const dom = new JSDOM("", { url: "https://example.invalid/?app-lifecycle-probe=1" });
Object.assign(globalThis, { window: dom.window });
const retained = Array.from({ length: 4096 }, () => createAppRenderToken()!);
try {
  // Keeping this cohort alive is deliberate: the probe must report the leak.
  for (const token of retained) commitAppRenderToken(token);
  const first = window.__reasonixAppLifecycle!.snapshot();
  assert.equal(first.liveRenderTokens, retained.length, "the oldest live references must not be evicted");
  commitAppRenderToken(retained[0]);
  assert.equal(window.__reasonixAppLifecycle!.snapshot().liveRenderTokens, retained.length,
    "StrictMode commit replay must not duplicate a presentation identity");
  trackAppOperation(1);
  trackAppOperation(-1);
  trackAppOperation(-1);
  assert.equal(window.__reasonixAppLifecycle!.snapshot().activeOperations, -1, "double cleanup must remain observable");
  trackAppSubscription(1);
  trackAppSubscription(-1);
  trackAppSubscription(-1);
  assert.equal(window.__reasonixAppLifecycle!.snapshot().activeSubscriptions, -1);
  console.log("PASS lifecycle probe exposes retained cohorts and duplicate cleanup");
} finally {
  dom.window.close();
}
