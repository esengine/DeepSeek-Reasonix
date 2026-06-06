/** Behavior-stability evaluation types — lightweight, local-first regression suite. */

export interface EvalScenario {
  id: string;
  name: string;
  category: "shell" | "context" | "integrity";
  /** True when the scenario calls the live LLM API (costs money). */
  requiresApi: boolean;
  run: () => Promise<EvalResult>;
}

export interface EvalResult {
  scenarioId: string;
  pass: boolean;
  durationMs: number;
  details: string;
}

export interface EvalReport {
  meta: {
    date: string;
    reasonixVersion: string;
    model?: string;
  };
  results: EvalResult[];
}
