import assert from "node:assert/strict";
import { chmodSync, existsSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

const candidate = "c46e3af1c2732fe2b3dedb0bd47eb39a629357d2";

// The fake npm records every dist-tag mutation so a test can assert not just
// the outcome but how many registry writes it took to get there — the whole
// point of the idempotent path is that a rerun performs none.
function run(
  expectedSha = candidate,
  publishedSha = candidate,
  staleReads = 0,
  { staging = false, forbidRemoval = false } = {},
) {
  const directory = mkdtempSync(join(tmpdir(), "reasonix-npm-alias-test-"));
  const npm = join(directory, "npm");
  const state = join(directory, "state");
  const writes = join(directory, "writes");
  writeFileSync(npm, `#!/usr/bin/env node
const fs = require("node:fs");
const args = process.argv.slice(2);
if (args[0] === "view" && args[1].includes("@1.19.2") && args.at(-1) === "--json") {
  const name = args[1].slice(0, -"@1.19.2".length);
  console.log(JSON.stringify({ name, version: "1.19.2", gitHead: ${JSON.stringify(publishedSha)}, reasonixCandidateSha: ${JSON.stringify(publishedSha)} }));
} else if (args[0] === "view" && args[2] === "dist-tags") {
  const statePath = process.env.NPM_FAKE_STATE;
  const reads = fs.existsSync(statePath) ? Number(fs.readFileSync(statePath, "utf8")) : 0;
  fs.writeFileSync(statePath, String(reads + 1));
  const stale = reads < Number(process.env.NPM_FAKE_STALE_READS || 0);
  const tags = stale
    ? { latest: "1.19.2", canary: "1.19.2-canary.1", next: "1.19.0-rc.3" }
    : { latest: "1.19.2", canary: "1.19.2", next: "1.19.2" };
  if (process.env.NPM_FAKE_STAGING === "1") tags["latest-staging"] = "1.19.2";
  console.log(JSON.stringify(tags));
} else if (args[0] === "dist-tag" && (args[1] === "add" || args[1] === "rm")) {
  fs.appendFileSync(process.env.NPM_FAKE_WRITES, JSON.stringify(args) + "\\n");
  if (args[1] === "rm" && process.env.NPM_FAKE_FORBID_REMOVAL === "1") {
    console.error("npm error code E403");
    console.error("npm error 403 Forbidden");
    process.exit(1);
  }
  process.exit(0);
} else {
  console.error("unexpected npm arguments", JSON.stringify(args));
  process.exit(2);
}
`);
  chmodSync(npm, 0o755);
  const result = spawnSync(process.execPath, ["scripts/finalize-npm-official-release.mjs", "1.19.2"], {
    cwd: new URL("..", import.meta.url),
    encoding: "utf8",
    env: {
      ...process.env,
      EXPECTED_SHA: expectedSha,
      NPM_FAKE_STATE: state,
      NPM_FAKE_STALE_READS: String(staleReads),
      NPM_FAKE_STAGING: staging ? "1" : "0",
      NPM_FAKE_FORBID_REMOVAL: forbidRemoval ? "1" : "0",
      NPM_FAKE_WRITES: writes,
      NPM_TAG_VERIFY_DELAY_MS: "1",
      PATH: `${directory}:${process.env.PATH}`,
    },
  });
  result.writes = existsSync(writes)
    ? readFileSync(writes, "utf8").trim().split("\n").filter(Boolean).map((line) => JSON.parse(line))
    : [];
  return result;
}

test("completes after exact package provenance validation", () => {
  const result = run();
  assert.equal(result.status, 0, result.stderr);
});

test("waits for npm alias propagation before continuing", () => {
  const result = run(candidate, candidate, 2);
  assert.equal(result.status, 0, result.stderr);
});

test("fails closed before alias mutation when provenance differs", () => {
  const result = run(candidate, "a".repeat(40));
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /does not match/);
  assert.deepEqual(result.writes, [], "provenance mismatch must not touch the registry");
});

// Recovery reruns this after the aliases are already correct — including after a
// manual realignment, which is exactly what happens when the automation token is
// refused. Writing anyway turned a rerun with nothing to do into a 403 (#7342
// cluster), so an already-aligned release must perform zero registry writes.
test("writes nothing when the official aliases already point at the release", () => {
  const result = run();
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(result.writes, [], `unexpected registry writes: ${JSON.stringify(result.writes)}`);
});

// Only the aliases that are actually behind get written.
test("writes only the aliases that are missing", () => {
  const result = run(candidate, candidate, 1);
  assert.equal(result.status, 0, result.stderr);
  const added = result.writes.filter((args) => args[1] === "add").map((args) => args.at(-1));
  assert.ok(added.length > 0, "a stale alias must still be written");
  assert.ok(!added.includes("latest"), `latest was already current: ${JSON.stringify(result.writes)}`);
  assert.deepEqual([...new Set(added)].sort(), ["canary", "next"]);
});

// The publisher stages under "<dist-tag>-staging"; cleanup used to name
// "official-staging", which nothing creates, so a leftover staging alias
// survived on the registry.
test("removes the staging alias the publisher actually creates", () => {
  const result = run(candidate, candidate, 0, { staging: true });
  assert.equal(result.status, 0, result.stderr);
  const removed = result.writes.filter((args) => args[1] === "rm");
  assert.equal(removed.length, 7, `expected one removal per package: ${JSON.stringify(removed)}`);
  for (const args of removed) {
    assert.equal(args.at(-1), "latest-staging");
  }
});

test("completes when npm forbids removal after official aliases converge", () => {
  const result = run(candidate, candidate, 0, {
    staging: true,
    forbidRemoval: true,
  });
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stderr, /official aliases are already verified/);
  const removals = result.writes.filter((args) => args[1] === "rm");
  assert.equal(removals.length, 7);
});
