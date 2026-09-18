import assert from "node:assert/strict";
import { join } from "node:path";
import { test } from "node:test";
import { iconCandidates, shouldOverrideDockIcon } from "./icons.js";

const desktop = "/repo/desktop";

test("a packaged bundle keeps its own Dock icon so macOS renders it", () => {
  // Overriding it would drop the macOS 26 Liquid Glass treatment.
  assert.equal(shouldOverrideDockIcon({ platform: "darwin", packaged: true }), false);
});

test("only the unpackaged development shell overrides the Dock icon", () => {
  assert.equal(shouldOverrideDockIcon({ platform: "darwin", packaged: false }), true);
  assert.equal(shouldOverrideDockIcon({ platform: "win32", packaged: false }), false);
  assert.equal(shouldOverrideDockIcon({ platform: "linux", packaged: false }), false);
});

test("the macOS app candidate prefers the safe-area asset", () => {
  const icons = iconCandidates({ platform: "darwin", appPath: join(desktop, "electron"), resourcesPath: "/unused", packaged: false });
  assert.deepEqual(icons.app, [
    join(desktop, "build", "darwin", "appicon.png"),
    join(desktop, "build", "appicon.png"),
  ]);
});

test("non-darwin platforms resolve the window icon from the full-canvas asset", () => {
  for (const platform of ["win32", "linux"] as const) {
    const icons = iconCandidates({ platform, appPath: join(desktop, "electron"), resourcesPath: "/unused", packaged: false });
    assert.deepEqual(icons.app, [
      join(desktop, "build", "linux", "icons", "hicolor", "256x256", "apps", "reasonix-desktop.png"),
      join(desktop, "build", "appicon.png"),
    ]);
  }
});
