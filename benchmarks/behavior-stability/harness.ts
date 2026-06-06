/** Evaluation harness — runs scenarios and collects results. */

import type { EvalReport, EvalResult, EvalScenario } from "./types.js";

export async function runScenarios(
  scenarios: EvalScenario[],
  opts: {
    onResult?: (r: EvalResult) => void;
  } = {},
): Promise<EvalReport> {
  const results: EvalResult[] = [];
  for (const sc of scenarios) {
    const t0 = Date.now();
    let result: EvalResult;
    try {
      result = await sc.run();
      result.durationMs = Date.now() - t0;
    } catch (err) {
      result = {
        scenarioId: sc.id,
        pass: false,
        durationMs: Date.now() - t0,
        details: `UNEXPECTED ERROR: ${(err as Error).message}`,
      };
    }
    results.push(result);
    opts.onResult?.(result);
  }

  // Dynamic import avoids bundling VERSION into the harness module.
  const { VERSION } = await import("../../src/version.js");
  return {
    meta: {
      date: new Date().toISOString(),
      reasonixVersion: VERSION,
    },
    results,
  };
}
