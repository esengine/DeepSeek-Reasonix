import assert from "node:assert/strict";
import test from "node:test";
import { fingerprintFiles, outsideSigningContract } from "./verify-candidate-signing-repair.mjs";

const original = `jobs:\n  resolve:\n    runs-on: ubuntu-latest\n  signing-contract:\n    steps:\n      - run: original gate\n  build:\n    runs-on: macos-latest\n  publish:\n    runs-on: ubuntu-latest\n`;

test("a control repair may change the signing gate but not build or publication", () => {
  const repaired = original.replace("original gate", "verified candidate gate");
  assert.equal(outsideSigningContract(original), outsideSigningContract(repaired));
  assert.notEqual(outsideSigningContract(original), outsideSigningContract(repaired.replace("macos-latest", "windows-latest")));
  assert.throws(() => outsideSigningContract("jobs:\n  signing-contract:\n"), /boundaries/);
});

test("the fingerprint file list must include the Desktop workflow", () => {
  assert.deepEqual(fingerprintFiles("fingerprint_files:\n  - .github/workflows/release-desktop.yml\n  - scripts/sign-certum.ps1\n"),
    [".github/workflows/release-desktop.yml", "scripts/sign-certum.ps1"]);
  assert.throws(() => fingerprintFiles("fingerprint_files:\n  - scripts/sign-certum.ps1\n"), /absent/);
});
