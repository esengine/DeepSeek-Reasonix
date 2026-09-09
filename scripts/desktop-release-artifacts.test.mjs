import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { collect, pack, platforms, releaseIdentity } from "./desktop-release-artifacts.mjs";

const env = {
  RELEASE_SOURCE_SHA: "a".repeat(40), RELEASE_CONTROL_SHA: "b".repeat(40),
  RELEASE_TAG: "desktop-v1.2.3", RELEASE_VERSION: "v1.2.3", RELEASE_CHANNEL: "stable",
  RELEASE_SIGNING_FINGERPRINT: "contract-digest", RELEASE_ARTIFACT_PREFIX: "desktop-123-1-preflight",
  GITHUB_RUN_ID: "123", GITHUB_RUN_ATTEMPT: "2",
};
const identity = releaseIdentity(env);
function fixture(t) {
  const root = mkdtempSync(path.join(tmpdir(), "reasonix-signed-handoff-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const bundles = path.join(root, "bundles");
  mkdirSync(bundles);
  for (const platform of platforms) {
    const source = path.join(root, platform);
    mkdirSync(source);
    writeFileSync(path.join(source, `${platform}.zip`), `signed:${platform}`);
    writeFileSync(path.join(source, `${platform}.zip.minisig`), `signature:${platform}`);
    pack(source, path.join(bundles, `${identity.prefix}-${platform}`), platform, identity);
  }
  return { bundles, target: path.join(root, "dist"), first: path.join(bundles, `${identity.prefix}-${platforms[0]}`) };
}

test("same-run failed-job retry preserves the exact signed bytes of all platforms", t => {
  const f = fixture(t);
  collect(f.bundles, f.target, identity);
  for (const platform of platforms) {
    assert.equal(readFileSync(path.join(f.target, `${platform}.zip`), "utf8"), `signed:${platform}`);
    assert.equal(readFileSync(path.join(f.target, `${platform}.zip.minisig`), "utf8"), `signature:${platform}`);
  }
});

test("another run, future attempt and missing identity cannot be reused", () => {
  for (const change of [{ GITHUB_RUN_ID: "124" }, { RELEASE_ARTIFACT_PREFIX: "desktop-123-3-preflight" },
    { RELEASE_SOURCE_SHA: "" }, { RELEASE_CONTROL_SHA: "main-v2" }, { GITHUB_RUN_ATTEMPT: "" }]) {
    assert.throws(() => releaseIdentity({ ...env, ...change }));
  }
});

for (const field of ["sourceSHA", "controlSHA", "tag", "version", "channel", "signingFingerprint", "prefix"]) {
  test(`reject mismatched ${field} before staging publication`, t => {
    const f = fixture(t);
    assert.throws(() => collect(f.bundles, f.target, { ...identity, [field]: "different" }));
    assert.throws(() => readFileSync(f.target), { code: "ENOENT" });
  });
}

for (const mutation of ["payload", "signature", "missing-platform", "extra-platform", "symlink", "extra-file"]) {
  test(`reject ${mutation} corruption`, t => {
    const f = fixture(t);
    const file = path.join(f.first, "files", `${platforms[0]}.zip`);
    if (mutation === "payload") writeFileSync(file, "corrupted");
    if (mutation === "signature") writeFileSync(`${file}.minisig`, "corrupted");
    if (mutation === "missing-platform") rmSync(f.first, { recursive: true });
    if (mutation === "extra-platform") mkdirSync(path.join(f.bundles, "unexpected"));
    if (mutation === "extra-file") writeFileSync(path.join(f.first, "files", "extra"), "unbound");
    if (mutation === "symlink") { rmSync(file); symlinkSync(`${file}.minisig`, file); }
    assert.throws(() => collect(f.bundles, f.target, identity));
    assert.throws(() => readFileSync(f.target), { code: "ENOENT" });
  });
}

test("an unsigned source cannot become a bundle", t => {
  const f = fixture(t);
  const source = path.join(f.first, "files");
  rmSync(path.join(source, `${platforms[0]}.zip.minisig`));
  assert.throws(() => pack(source, f.target, platforms[0], identity), /missing payload or signature/);
});

test("a failed platform can be replaced on retry while successful platforms retain their bytes", t => {
  const f = fixture(t);
  const manifestFile = path.join(f.first, "identity.json");
  const manifest = JSON.parse(readFileSync(manifestFile, "utf8"));
  manifest.buildAttempt = "2";
  writeFileSync(manifestFile, JSON.stringify(manifest));
  assert.throws(() => collect(f.bundles, f.target, identity, "1"), /producer attempt/);
  collect(f.bundles, f.target, identity, "2");
  assert.equal(readFileSync(path.join(f.target, `${platforms[0]}.zip`), "utf8"), `signed:${platforms[0]}`);
});

test("platform bundles cannot overwrite each other's filenames", t => {
  const f = fixture(t);
  const second = path.join(f.bundles, `${identity.prefix}-${platforms[1]}`);
  rmSync(second, { recursive: true });
  pack(path.join(f.first, "files"), second, platforms[1], identity);
  assert.throws(() => collect(f.bundles, f.target, identity), /duplicate artifact/);
});
