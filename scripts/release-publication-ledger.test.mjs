import assert from "node:assert/strict";
import test from "node:test";
import { spawnSync } from "node:child_process";
import { mkdtempSync, writeFileSync, rmSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { createCoreLedger, createSiteLedger, mergeLedgers, ownsPublicSite } from "./release-publication-ledger.mjs";

const sha = "a".repeat(40);
const release = { isDraft: false, isPrerelease: false, assets: [{ name: "asset.zip", size: 42, digest: "sha256:abc" }] };

test("records immutable files for the GitHub releases this line owns", () => {
  const core = createCoreLedger({
    version: "1.2.3", sourceSHA: sha, operation: "publish",
    cliRelease: release, desktopRelease: release,
  });
  assert.equal(core.surfaces.tags.items.length, 3);
  assert.deepEqual(Object.keys(core.surfaces).sort(), ["cli", "desktop", "tags"]);
  assert.equal(core.surfaces.cli.assets[0].digest, "sha256:abc");
  assert.throws(() => createCoreLedger({
    version: "1.2.3", sourceSHA: sha, operation: "publish",
    cliRelease: { ...release, isDraft: true }, desktopRelease: release,
  }), /CLI release is not a public final release/);
});

test("site evidence merges only for the same candidate", () => {
  const core = createCoreLedger({
    version: "1.2.3", sourceSHA: sha, operation: "publish",
    cliRelease: release, desktopRelease: release,
  });
  const site = createSiteLedger({
    version: "1.2.3", sourceSHA: sha, operation: "publish", manifest: { version: "v1.2.3" },
  });
  assert.equal(mergeLedgers(core, site).surfaces.homepage.state, "public-entry-updated");
  assert.equal(mergeLedgers(core, site).surfaces.homebrew, undefined);
  assert.throws(() => mergeLedgers(core, { ...site, sourceSHA: "b".repeat(40) }), /one release/);
});

test("site ownership requires an exact release or a proven newer recovery pointer", () => {
  for (const operation of ["publish", "recover"]) {
    assert.equal(ownsPublicSite("1.2.3", operation, { version: "v1.2.3" }), true);
    for (const version of [undefined, "", "v1.2.2", "1.2.3", "v1.2.3-beta.1"]) {
      assert.throws(() => ownsPublicSite("1.2.3", operation, { version }));
    }
  }
  assert.equal(ownsPublicSite("1.2.3", "recover", { version: "v1.10.0" }), false);
  assert.throws(() => ownsPublicSite("1.2.3", "publish", { version: "v1.10.0" }));
  assert.throws(() => ownsPublicSite("1.2.3", "unknown", { version: "v1.2.3" }));
});

test("actual site observation fails closed on HTTP errors and malformed or stale responses", () => {
  const directory = mkdtempSync(join(tmpdir(), "release-site-observation-"));
  try {
    writeFileSync(join(directory, "go"), '#!/bin/sh\nif [ "$RESPONSE_STATUS" != 0 ]; then exit "$RESPONSE_STATUS"; fi\nprintf "%s" "$RESPONSE_BODY" > "$4"\n', { mode: 0o755 });
    const helper = fileURLToPath(new URL("./observe-release-site.sh", import.meta.url));
    for (const [status, body, expected] of [
      [22, "", null], [22, '{"version":"v1.3.0"}', null],
      [0, "", null], [0, "<html>Forbidden</html>", null],
      [0, '{"version":"v1.2.2"}', null],
      [0, '{"version":"v1.2.3"}', "true"],
      [0, '{"version":"v1.10.0"}', "false"],
    ]) {
      const result = spawnSync("bash", [helper, "1.2.3", "recover"], {
        encoding: "utf8", env: { ...process.env, PATH: `${directory}:${process.env.PATH}`, RESPONSE_STATUS: String(status), RESPONSE_BODY: body },
      });
      if (expected === null) {
        assert.notEqual(result.status, 0, `must reject HTTP ${status}: ${body}`);
        assert.equal(result.stdout, "");
      } else {
        assert.equal(result.status, 0, result.stderr);
        assert.equal(result.stdout.trim(), expected);
      }
    }
    for (const file of ["./sync-release-site.sh", "./verify-stable-release-artifacts.sh"]) {
      assert.match(readFileSync(new URL(file, import.meta.url), "utf8"), /observe-release-site\.sh/);
    }
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
