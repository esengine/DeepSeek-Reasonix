import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import { APP_USER_MODEL_ID, applyAppUserModelId } from "./appIdentity.js";

function windowsApp() {
  const applied: string[] = [];
  return { applied, app: { setAppUserModelId: (id: string) => applied.push(id) } };
}

test("Windows adopts the launcher's shared AppUserModelID before any window exists", () => {
  const { applied, app } = windowsApp();
  applyAppUserModelId(app, "win32");
  assert.deepEqual(applied, [APP_USER_MODEL_ID]);
});

test("other platforms never touch the Windows identity", () => {
  for (const platform of ["darwin", "linux"] as const) {
    const { applied, app } = windowsApp();
    applyAppUserModelId(app, platform);
    assert.deepEqual(applied, [], `${platform} must not set an AppUserModelID`);
  }
});

test("main applies the identity at load time rather than after readiness", () => {
  const source = readFileSync(fileURLToPath(new URL("./index.ts", import.meta.url)), "utf8");
  const applied = source.indexOf("applyAppUserModelId(app, process.platform)");
  assert.notEqual(applied, -1, "index.ts must apply the AppUserModelID");
  // app.whenReady() creates the window, so a later call would miss the taskbar.
  const ready = source.indexOf("app.whenReady()");
  assert.notEqual(ready, -1, "index.ts must still gate startup on app readiness");
  assert.ok(applied < ready, "the AppUserModelID must be applied before app.whenReady()");
});
