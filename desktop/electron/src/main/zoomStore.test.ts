import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, writeFile, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { AppZoomStore, normalizeAppZoom } from "./zoomStore.js";

test("normalizes app zoom and defaults safely", () => {
  assert.equal(normalizeAppZoom(Number.NaN), 1);
  assert.equal(normalizeAppZoom(0.71), 0.7);
  assert.equal(normalizeAppZoom(3), 2);
});

test("migrates only explicitly user-owned legacy zoom", async () => {
  const root = await mkdtemp(join(tmpdir(), "reasonix-zoom-"));
  const legacy = join(root, "desktop-zoom.json");
  await writeFile(legacy, JSON.stringify({ zoomFactor: 0.7 }));
  const store = new AppZoomStore(join(root, "electron-app-zoom.json"), legacy);
  assert.equal((await store.load()).appZoomFactor, 1);
  await writeFile(legacy, JSON.stringify({ zoomFactor: 0.7, source: "user" }));
  const second = new AppZoomStore(join(root, "electron-app-zoom-2.json"), legacy);
  assert.equal((await second.load()).appZoomFactor, 0.7);
});

test("persists user value and reset", async () => {
  const root = await mkdtemp(join(tmpdir(), "reasonix-zoom-"));
  const file = join(root, "nested", "zoom.json");
  const store = new AppZoomStore(file);
  await store.set(0.85);
  assert.equal((await store.load()).appZoomFactor, 0.85);
  await store.reset();
  assert.deepEqual(JSON.parse(await readFile(file, "utf8")), { version: 1, appZoomFactor: 1, source: "user" });
});
