import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import {
  type LifecycleIntentCorpusCase,
  evaluateLifecycleIntentCorpus,
  formatLifecycleIntentReport,
} from "../src/code/lifecycle-intent-runner.js";

const FIXTURE_PATH = resolve("tests/fixtures/lifecycle-intent-corpus.json");
const NPX_BIN = process.platform === "win32" ? "npx.cmd" : "npx";

function loadFixture(): LifecycleIntentCorpusCase[] {
  return JSON.parse(readFileSync(FIXTURE_PATH, "utf8")) as LifecycleIntentCorpusCase[];
}

describe("lifecycle intent corpus runner", () => {
  it("evaluates the checked-in corpus with a stable report", () => {
    const report = evaluateLifecycleIntentCorpus(loadFixture());

    expect(report.total).toBeGreaterThanOrEqual(12);
    expect(report.failed).toBe(0);
    expect(report.passed).toBe(report.total);
    expect(report.recommendationAccuracy).toBe(1);
    expect(report.signalRecall).toBe(1);
    expect(formatLifecycleIntentReport(report)).toContain(
      `${report.passed}/${report.total} passed`,
    );
  });

  it("keeps corpus evaluation advisory-only", () => {
    const report = evaluateLifecycleIntentCorpus(loadFixture());

    expect(report.results.every((item) => item.modelVisible === false)).toBe(true);
    expect(report.results.every((item) => item.enforced === false)).toBe(true);
  });

  it("runs as an offline script without requiring an API key", () => {
    const result = spawnSync(NPX_BIN, ["tsx", "scripts/evaluate-lifecycle-intent.mts", "--json"], {
      encoding: "utf8",
      env: {
        ...process.env,
        DEEPSEEK_API_KEY: "",
      },
      timeout: 20_000,
    });

    expect(result.status).toBe(0);
    expect(result.stderr).toBe("");
    expect(JSON.parse(result.stdout)).toMatchObject({
      failed: 0,
      recommendationAccuracy: 1,
      signalRecall: 1,
    });
  });
});
