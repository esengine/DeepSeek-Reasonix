// Anonymous geometry-only regressions distilled from the three field reports.

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { isSupportedFrontendDiagnosticSchemaVersion } from "../lib/frontendDiagnostics";

type Fixture = {
  schemaVersion: number;
  name: string;
  viewport: number;
  samples: Array<{ top: number; height: number }>;
  expected: { stableHeight?: number; minimumCollapse?: number; minimumCorrection?: number; maximumReverse: number };
};

const fixtureDir = join(dirname(fileURLToPath(import.meta.url)), "../__fixtures__/transcript-scroll");
const names = ["reader-reverse-jump-v1.json", "reasoning-extent-cycle-v1.json", "false-bottom-collapse-v2.json"];
for (const name of names) {
  const fixture = JSON.parse(readFileSync(join(fixtureDir, name), "utf8")) as Fixture;
  assert.equal(isSupportedFrontendDiagnosticSchemaVersion(fixture.schemaVersion), true, `${fixture.name} uses a supported replay schema`);
  const heights = fixture.samples.map((sample) => sample.height);
  const tops = fixture.samples.map((sample) => sample.top);
  const collapse = Math.max(...heights) - Math.min(...heights);
  const reverse = Math.max(0, ...tops.slice(1).map((top, index) => tops[index] - top));
  if (fixture.expected.minimumCollapse !== undefined) assert.ok(collapse >= fixture.expected.minimumCollapse, `${fixture.name} preserves the reported extent collapse`);
  if (fixture.expected.minimumCorrection !== undefined) assert.ok(reverse >= fixture.expected.minimumCorrection, `${fixture.name} preserves the reported reader reversal`);
  if (fixture.expected.stableHeight !== undefined) assert.equal(Math.max(...heights), fixture.expected.stableHeight, `${fixture.name} preserves the stable extent`);
  assert.ok(fixture.samples.every((sample) => Object.keys(sample).every((key) => key === "top" || key === "height")), `${fixture.name} contains geometry only`);
}

console.log("transcript anonymous geometry replay fixtures passed");
