import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync, existsSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { GraphicsSettingsStore, loadGraphicsBootstrap } from "./graphics.js";

const home = () => mkdtempSync(join(tmpdir(), "reasonix-graphics-"));

test("defaults to enabled without creating a config", () => {
  const b = loadGraphicsBootstrap(home(), {}, []);
  assert.equal(b.state.hardwareAcceleration, true);
  assert.equal(b.shouldDisable, false);
  assert.equal(existsSync(b.configPath), false);
});

test("environment and command-line overrides disable without changing the saved preference", async () => {
  const root = home();
  const b = loadGraphicsBootstrap(root, {}, []);
  const store = new GraphicsSettingsStore(b.configPath, b);
  await store.setHardwareAcceleration(true);
  const overridden = loadGraphicsBootstrap(root, { REASONIX_DISABLE_GPU: "1" }, []);
  assert.equal(overridden.state.override, "environment");
  assert.equal(overridden.state.startupEnabled, false);
  assert.equal(loadGraphicsBootstrap(root, {}, ["--disable-gpu"]).state.override, "command-line");
  assert.equal(JSON.parse(readFileSync(overridden.configPath, "utf8")).hardwareAcceleration, true);
});

test("preserves unknown fields and rejects unknown versions", async () => {
  const root = home();
  const path = join(root, "graphics.json");
  const b = loadGraphicsBootstrap(root, {}, []);
  const store = new GraphicsSettingsStore(path, b);
  await store.setHardwareAcceleration(false);
  writeFileSync(path, JSON.stringify({ version: 1, hardwareAcceleration: false, future: { keep: true } }));
  const reread = loadGraphicsBootstrap(root, {}, []);
  const next = new GraphicsSettingsStore(path, reread);
  await next.setHardwareAcceleration(true);
  assert.deepEqual(JSON.parse(readFileSync(path, "utf8")).future, { keep: true });
  writeFileSync(path, JSON.stringify({ version: 9, hardwareAcceleration: false }));
  const unknown = loadGraphicsBootstrap(root, {}, []);
  assert.equal(unknown.state.writable, false);
  await assert.rejects(() => new GraphicsSettingsStore(path, unknown).setHardwareAcceleration(true));
});

test("backs up repeated invalid files with unique names before recovery", async () => {
  const root = home();
  const path = join(root, "graphics.json");
  writeFileSync(path, "not json");
  const first = loadGraphicsBootstrap(root, {}, []);
  await new GraphicsSettingsStore(path, first).setHardwareAcceleration(false);
  writeFileSync(path, "still not json");
  const second = loadGraphicsBootstrap(root, {}, []);
  await new GraphicsSettingsStore(path, second).setHardwareAcceleration(true);
  const backups = readdirSync(root).filter((name) => name.startsWith("graphics.json.invalid"));
  assert.equal(backups.length, 2);
  assert.equal(JSON.parse(readFileSync(path, "utf8")).hardwareAcceleration, true);
});
