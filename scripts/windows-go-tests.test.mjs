import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { isolatedGroups, selectPackages, testArgs } from "./windows-go-tests.mjs";

const packages = ["reasonix/cmd/reasonix", "reasonix/internal/agent", "reasonix/internal/agent/testutil",
  "reasonix/internal/agentpreset", "reasonix/internal/boot", "reasonix/internal/control",
  "reasonix/internal/control/child", "reasonix/internal/extension/sidecar", "reasonix/internal/proc",
  "reasonix/internal/newpackage", "reasonix/tools/repolint"];

test("the full Windows groups cover every package exactly once, including new packages", () => {
  const grouped = ["full", ...isolatedGroups].flatMap(group => selectPackages(packages, group));
  assert.deepEqual(grouped.toSorted(), packages.toSorted());
  assert.equal(new Set(grouped).size, grouped.length);
  assert.ok(selectPackages(packages, "full").includes("reasonix/internal/agentpreset"));
});

test("PR smoke keeps platform coverage without duplicating isolated suites", () => {
  assert.deepEqual(selectPackages(packages, "smoke"), ["reasonix/cmd/reasonix", "reasonix/internal/extension/sidecar", "reasonix/internal/proc"]);
  for (const group of isolatedGroups) {
    assert.deepEqual(testArgs(packages, group).slice(0, 4), ["test", "-p", "1", "-timeout=8m"]);
  }
  assert.deepEqual(testArgs(packages, "full").slice(0, 4), ["test", "-p", "4", "-timeout=8m"]);
  assert.throws(() => testArgs(packages, "typo"), /Unknown/);
  assert.throws(() => testArgs([], "full"), /Empty/);
});

test("CI invokes every isolated group and both residual entrypoints", () => {
  const source = readFileSync(new URL("../.github/workflows/ci.yml", import.meta.url), "utf8");
  for (const group of ["full", "smoke", "control"]) {
    assert.match(source, new RegExp(`run: node scripts/windows-go-tests\\.mjs ${group}\\n`));
  }
  const isolated = source.match(/\n  windows-isolated:\n([\s\S]*?)(?=\n  [a-z][a-z0-9-]*:|$)/)?.[1];
  assert.ok(isolated);
  const matrix = isolated.match(/group: \[([^\]]+)\]/)[1].split(",").map(value => value.trim());
  assert.deepEqual([...matrix, "control"].toSorted(), isolatedGroups.toSorted());
  assert.match(isolated, /run: node scripts\/windows-go-tests\.mjs \$\{\{ matrix.group \}\}/);
  assert.match(isolated, /fail-fast: false/);
  assert.match(isolated, /actions\/setup-node@v7/);
});
