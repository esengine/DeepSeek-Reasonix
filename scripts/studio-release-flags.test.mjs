import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const script = path.join(path.dirname(fileURLToPath(import.meta.url)), "studio-release-flags.sh");

const tags = [
  "studio-v2.9.0",
  "studio-v2.10.0",
  "studio-v2.28.0",
  "studio-v2.29.0-rc.1",
  "studio-v2.30.0-preview.2",
  "studio-v2.27.3",
];

function flags(tag, candidate, listed = tags) {
  const run = spawnSync("bash", [script, tag, candidate], { input: listed.join("\n") + "\n", encoding: "utf8" });
  assert.equal(run.status, 0, run.stderr);
  return run.stdout.trim().split("\n");
}

test("the highest stable tag takes latest and is not a prerelease", () => {
  assert.deepEqual(flags("studio-v2.28.0", "false"), ["--latest"]);
});

test("an older stable tag does not take latest, numerically ordered", () => {
  assert.deepEqual(flags("studio-v2.27.3", "false"), ["--latest=false"]);
  assert.deepEqual(flags("studio-v2.9.0", "false"), ["--latest=false"]);
});

test("a candidate stays a prerelease even above every stable tag", () => {
  assert.deepEqual(flags("studio-v2.29.0-rc.1", "true"), ["--prerelease", "--latest=false"]);
  assert.deepEqual(flags("studio-v2.30.0-preview.2", "true"), ["--prerelease", "--latest=false"]);
});

test("a prerelease tag never counts as the highest stable tag", () => {
  assert.deepEqual(flags("studio-v2.28.0", "false", tags.concat("studio-v2.99.0-rc.1")), ["--latest"]);
});

test("the only stable tag is latest, and a tag list without stable tags takes none", () => {
  assert.deepEqual(flags("studio-v2.0.0", "false", ["studio-v2.0.0"]), ["--latest"]);
  assert.deepEqual(flags("studio-v2.0.0", "false", ["studio-v2.1.0-rc.1"]), ["--latest=false"]);
});
