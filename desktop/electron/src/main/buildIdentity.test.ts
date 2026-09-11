import assert from "node:assert/strict";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test, type TestContext } from "node:test";
import { loadBuildIdentity } from "./buildIdentity.js";
import { buildHelloParams } from "./handshake.js";

function resources(t: TestContext, content?: unknown): string {
  const dir = mkdtempSync(join(tmpdir(), "reasonix-identity-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  if (content !== undefined) writeFileSync(join(dir, "build.json"), JSON.stringify(content));
  return dir;
}

for (const version of ["v1.38.5", "v1.38.6-rc.1", "v0.0.0-ci"]) {
  test(`packaged hello preserves the complete ${version} identity without environment overrides`, (t) => {
    const build = { version, channel: "canary", commit: "abc123def456" };
    const dir = resources(t, { schemaVersion: 1, ...build, electron: "44.2.0", platform: "windows/amd64" });
    const identity = loadBuildIdentity(true, dir, { REASONIX_CHANNEL: "wrong", REASONIX_COMMIT: "wrong" });
    const hello = buildHelloParams({ ...identity, contractDigest: "sha256:fixture", hostVersion: "44.2.0", chromeVersion: "152", platform: "win32", arch: "x64", home: dir, dev: false });
    assert.deepEqual(hello.build, build);
    assert.equal(hello.instance.dev, false);
  });
}

test("unpackaged development needs no manifest", () => {
  assert.deepEqual(loadBuildIdentity(false, "unused", {}), { version: "dev", channel: "dev", commit: "dev" });
  assert.deepEqual(loadBuildIdentity(false, "unused", { REASONIX_CHANNEL: "canary", REASONIX_COMMIT: "local" }), { version: "dev", channel: "canary", commit: "local" });
});

test("missing or invalid packaged metadata cannot silently fall back to dev", (t) => {
  const good = { schemaVersion: 1, version: "v1.38.5", channel: "stable", commit: "abc123" };
  assert.throws(() => loadBuildIdentity(true, resources(t), {}), /ENOENT/);
  for (const bad of [null, [], {}, { ...good, schemaVersion: 2 }, { ...good, version: "dev" }, { ...good, version: "1.38.5" }, { ...good, version: "v1.38.5 " }, { ...good, channel: "" }, { ...good, commit: " " }]) {
    assert.throws(() => loadBuildIdentity(true, resources(t, bad), {}), /build/);
  }
  const dir = resources(t);
  writeFileSync(join(dir, "build.json"), "{");
  assert.throws(() => loadBuildIdentity(true, dir, {}), SyntaxError);
});
