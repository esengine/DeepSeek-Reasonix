import test from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { resolveReasonixBin, assertTrustedReasonixBin } from "../lib/resolveReasonixBin.mjs";

test("resolveReasonixBin prefers REASONIX_BIN when file exists", () => {
  const fake = path.join("/tmp", "reasonix");
  const bin = resolveReasonixBin({
    env: { REASONIX_BIN: fake },
    existsSync: (p) => p === fake,
    isFile: (p) => p === fake,
    which: () => null,
  });
  assert.equal(bin, path.resolve(fake));
});

test("resolveReasonixBin rejects untrusted basename even if REASONIX_BIN set", () => {
  assert.throws(
    () =>
      resolveReasonixBin({
        env: { REASONIX_BIN: "/evil/malware" },
        existsSync: () => true,
        isFile: () => true,
        which: () => null,
      }),
    /untrusted binary name/,
  );
});

test("resolveReasonixBin falls back to PATH which()", () => {
  const fromPath = "/usr/local/bin/reasonix";
  const bin = resolveReasonixBin({
    env: {},
    existsSync: (p) => p === fromPath,
    isFile: (p) => p === fromPath,
    which: (name) => (name === "reasonix" ? fromPath : null),
  });
  assert.equal(bin, fromPath);
});

test("assertTrustedReasonixBin rejects non-reasonix names", () => {
  assert.throws(() => assertTrustedReasonixBin("/bin/sh"), /untrusted/);
});
