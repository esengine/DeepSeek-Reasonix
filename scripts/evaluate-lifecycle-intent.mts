#!/usr/bin/env tsx

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import {
  type LifecycleIntentCorpusCase,
  evaluateLifecycleIntentCorpus,
  formatLifecycleIntentReport,
} from "../src/code/lifecycle-intent-runner.js";

const args = new Set(process.argv.slice(2));
const json = args.has("--json");
const fixtureArg = process.argv.slice(2).find((arg) => !arg.startsWith("-"));
const fixturePath = resolve(fixtureArg ?? "tests/fixtures/lifecycle-intent-corpus.json");
const cases = JSON.parse(readFileSync(fixturePath, "utf8")) as LifecycleIntentCorpusCase[];
const report = evaluateLifecycleIntentCorpus(cases);

if (json) {
  console.log(JSON.stringify(report, null, 2));
} else {
  console.log(formatLifecycleIntentReport(report));
}

if (report.failed > 0) {
  process.exitCode = 1;
}
