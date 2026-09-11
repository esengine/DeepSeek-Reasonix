import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { supersededLauncher } from "./recovery.js";

test("recovery re-reads current.json and selects only this installation's stable entry", (t) => {
  const root = mkdtempSync(join(tmpdir(), "reasonix-recovery-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "versions", "v1.38.5", "app"), { recursive: true });
  const shell = join(root, "versions", "v1.38.5", "app", "Reasonix.exe");
  const launcher = join(root, "reasonix-launcher.exe");
  writeFileSync(launcher, "fixture");
  const activate = (v: string, dir = `versions/${v}`) => writeFileSync(join(root, "current.json"), JSON.stringify({schemaVersion:1,activeVersion:v,activeDir:dir}));
  activate("v1.38.5"); assert.equal(supersededLauncher(shell,"v1.38.5"),undefined);
  activate("v1.38.7"); assert.equal(supersededLauncher(shell,"v1.38.5"),launcher);
  activate("v1.38.7","../other"); assert.equal(supersededLauncher(shell,"v1.38.5"),undefined);
});
