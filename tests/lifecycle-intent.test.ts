import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import type { LifecycleIntentCorpusCase } from "../src/code/lifecycle-intent-runner.js";
import { evaluateEngineeringLifecycleIntent } from "../src/code/lifecycle-intent.js";

const corpus = JSON.parse(
  readFileSync(resolve("tests/fixtures/lifecycle-intent-corpus.json"), "utf8"),
) as LifecycleIntentCorpusCase[];

describe("engineering lifecycle intent evaluator", () => {
  it.each(corpus)("classifies $id", ({ input, expectedRecommendation, expectedSignals }) => {
    const result = evaluateEngineeringLifecycleIntent(input);

    expect(result.recommendation).toBe(expectedRecommendation);
    expect(result.signals).toEqual(expect.arrayContaining(expectedSignals));
  });

  it("keeps lifecycle intent evaluation advisory-only", () => {
    const result = evaluateEngineeringLifecycleIntent("Refactor the parser across modules.");

    expect(result.modelVisible).toBe(false);
    expect(result.enforced).toBe(false);
  });
});
