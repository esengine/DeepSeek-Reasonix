import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

test("Electron main enforces isolation and single-instance", () => {
  const main = fs.readFileSync(path.join(root, "electron/main.mjs"), "utf8");
  assert.match(main, /contextIsolation:\s*true/);
  assert.match(main, /nodeIntegration:\s*false/);
  assert.match(main, /requestSingleInstanceLock/);
  assert.match(main, /pick-workspace|pickWorkspace|showOpenDialog/);
  assert.match(main, /crashRestartOnce/);
  assert.match(main, /openPath|open-log/);
  // Crash-restart must refresh lastStart + loadURL (not leave dead endpoint)
  assert.match(main, /onCrashRestarted/);
  assert.match(main, /applyServeReady/);
  assert.match(main, /loadURL/);
});

test("supervisor buildServeArgs contract embedded in source", () => {
  const sup = fs.readFileSync(path.join(root, "lib/serveSupervisor.mjs"), "utf8");
  assert.match(sup, /127\.0\.0\.1:0/);
  assert.match(sup, /--token-file/);
  assert.match(sup, /--port-file/);
  assert.match(sup, /--pid-file/);
  assert.match(sup, /--auth[\s\S]*token/);
  // Must refuse putting secrets on argv via --token flag in builder
  assert.match(sup, /must use --token-file only/);
});

test("capability gap and go/no-go artifacts exist", () => {
  assert.ok(fs.existsSync(path.join(root, "docs/CAPABILITY_GAP.md")));
  assert.ok(fs.existsSync(path.join(root, "docs/GO_NO_GO.md")));
  const gap = fs.readFileSync(path.join(root, "docs/CAPABILITY_GAP.md"), "utf8");
  assert.match(gap, /multiTab/);
  assert.match(gap, /terminal/);
  const gng = fs.readFileSync(path.join(root, "docs/GO_NO_GO.md"), "utf8");
  assert.match(gng, /NO-GO/);
  assert.match(gng, /Wails/);
});

test("preload exposes whitelist only", () => {
  const pre = fs.readFileSync(path.join(root, "electron/preload.cjs"), "utf8");
  assert.match(pre, /contextBridge\.exposeInMainWorld/);
  assert.match(pre, /getEndpoint/);
  assert.doesNotMatch(pre, /child_process/);
  assert.doesNotMatch(pre, /require\(['\"]fs['\"]\)/);
});
