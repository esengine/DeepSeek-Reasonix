import assert from "node:assert/strict";
import test from "node:test";
import { persistedWindowRect, restoreWindowRect, type WindowRect } from "./windowBounds.js";

const NORMAL: WindowRect = { x: 40, y: 50, width: 1240, height: 720 };
const FRAME: WindowRect = { x: -7, y: -7, width: 1722, height: 1034 };

function fakeWindow(state: { maximized?: boolean; minimized?: boolean; fullscreen?: boolean }) {
  return {
    isMaximized: () => state.maximized ?? false,
    isMinimized: () => state.minimized ?? false,
    isFullScreen: () => state.fullscreen ?? false,
    getBounds: () => FRAME,
    getNormalBounds: () => state.maximized || state.minimized || state.fullscreen ? NORMAL : FRAME,
  };
}

test("normal windows persist their live bounds", () => {
  assert.deepEqual(persistedWindowRect(fakeWindow({})), FRAME);
});

const SIZE = { width: 1100, height: 700, minWidth: 760, minHeight: 480 };
const PRIMARY = { x: 0, y: 0, width: 1707, height: 1019 };

test("valid custom restore geometry survives a maximized relaunch", () => {
  assert.deepEqual(restoreWindowRect(SIZE, { x: 40, y: 50 }, PRIMARY), { x: 40, y: 50, width: 1100, height: 700 });
});

test("legacy oversized maximized frame is fitted to the work area", () => {
  assert.deepEqual(restoreWindowRect({ ...SIZE, ...FRAME }, FRAME, PRIMARY), PRIMARY);
});

test("negative monitor origins and taskbar offsets remain valid DIP coordinates", () => {
  const area = { x: -1920, y: -1080, width: 1920, height: 1040 };
  assert.deepEqual(restoreWindowRect(SIZE, { x: -1800, y: -1000 }, area), { x: -1800, y: -1000, width: 1100, height: 700 });
});

test("unplugged display recenters without discarding a usable size", () => {
  assert.deepEqual(restoreWindowRect(SIZE, { x: -1800, y: -1000 }, PRIMARY), { x: 303, y: 159, width: 1100, height: 700 });
});

test("missing state centers defaults and small work areas bound even the minimum", () => {
  assert.deepEqual(restoreWindowRect(SIZE, undefined, PRIMARY), { x: 303, y: 159, width: 1100, height: 700 });
  const small = { x: 0, y: 40, width: 640, height: 400 };
  assert.deepEqual(restoreWindowRect(SIZE, undefined, small), small);
});

test("changed work area relocates only the overflowing edges", () => {
  assert.deepEqual(restoreWindowRect(SIZE, { x: 1000, y: 600 }, PRIMARY), { x: 607, y: 319, width: 1100, height: 700 });
});

test("maximized windows persist the restore rectangle, not the maximized frame", () => {
  assert.deepEqual(persistedWindowRect(fakeWindow({ maximized: true })), NORMAL);
});

test("minimized windows persist the restore rectangle, not the iconic position", () => {
  assert.deepEqual(persistedWindowRect(fakeWindow({ minimized: true })), NORMAL);
});

test("fullscreen windows persist the restore rectangle", () => {
  assert.deepEqual(persistedWindowRect(fakeWindow({ fullscreen: true })), NORMAL);
});
