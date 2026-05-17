/** Distillation telemetry — measures parent-log growth avoided per spawn. */

import { countTokensBounded } from "../tokenizer.js";

/** Minimum shape `computeSpawnDistillation` needs. `SubagentResult` matches structurally; declaring a local interface avoids a stats ↔ subagent ↔ loop import cycle. */
export interface SubagentResultLike {
  output: string;
  costUsd: number;
  usage: { completionTokens: number };
}

export interface SpawnDistillation {
  completionTokens: number;
  outputTokens: number;
  /** `completionTokens − outputTokens`, clamped to 0. Lower bound — ignores tool-result tokens that would also have landed in the parent log inline. */
  savingsTokens: number;
  /** `outputTokens / completionTokens`; 1 when completion is 0. Lower is more distilled; ≥1 means writes / passthrough. */
  compressionRatio: number;
  /** True iff `output.trim().length > 0`. */
  hasOutput: boolean;
  costUsd: number;
}

export function computeSpawnDistillation(result: SubagentResultLike): SpawnDistillation {
  const outputTokens = countTokensBounded(result.output);
  const completionTokens = result.usage.completionTokens;
  const savingsTokens = Math.max(0, completionTokens - outputTokens);
  const compressionRatio = completionTokens > 0 ? outputTokens / completionTokens : 1;
  return {
    completionTokens,
    outputTokens,
    savingsTokens,
    compressionRatio,
    hasOutput: result.output.trim().length > 0,
    costUsd: result.costUsd,
  };
}

export interface SubagentSessionSummary {
  spawnCount: number;
  usefulSpawnCount: number;
  /** `usefulSpawnCount / spawnCount`; 0 when no spawns. */
  successRate: number;
  totalCompletionTokens: number;
  totalOutputTokens: number;
  totalSavingsTokens: number;
  /** Weighted by completion tokens — fair vs. naive mean of ratios. */
  aggregateCompressionRatio: number;
  totalCostUsd: number;
}

export function summarizeSubagentSession(spawns: SpawnDistillation[]): SubagentSessionSummary {
  const spawnCount = spawns.length;
  if (spawnCount === 0) {
    return {
      spawnCount: 0,
      usefulSpawnCount: 0,
      successRate: 0,
      totalCompletionTokens: 0,
      totalOutputTokens: 0,
      totalSavingsTokens: 0,
      aggregateCompressionRatio: 1,
      totalCostUsd: 0,
    };
  }
  let usefulSpawnCount = 0;
  let totalCompletionTokens = 0;
  let totalOutputTokens = 0;
  let totalSavingsTokens = 0;
  let totalCostUsd = 0;
  for (const s of spawns) {
    if (s.hasOutput) usefulSpawnCount++;
    totalCompletionTokens += s.completionTokens;
    totalOutputTokens += s.outputTokens;
    totalSavingsTokens += s.savingsTokens;
    totalCostUsd += s.costUsd;
  }
  const aggregateCompressionRatio =
    totalCompletionTokens > 0 ? totalOutputTokens / totalCompletionTokens : 1;
  return {
    spawnCount,
    usefulSpawnCount,
    successRate: usefulSpawnCount / spawnCount,
    totalCompletionTokens,
    totalOutputTokens,
    totalSavingsTokens,
    aggregateCompressionRatio,
    totalCostUsd,
  };
}

export const DEFAULT_SPAWN_STORM_THRESHOLD = 3;

export function countSpawnStorms(
  spawnsByTurn: ReadonlyArray<ReadonlyArray<SpawnDistillation>>,
  threshold: number = DEFAULT_SPAWN_STORM_THRESHOLD,
): number {
  let storms = 0;
  for (const turn of spawnsByTurn) {
    if (turn.length >= threshold) storms++;
  }
  return storms;
}

/** Live collector — append every spawn result, query aggregates whenever. Bind `record` and pass as `onSpawnComplete` to `registerSubagentTool` for automatic capture. */
export class SubagentTelemetry {
  private readonly _spawns: SpawnDistillation[] = [];
  private readonly _byTurn: SpawnDistillation[][] = [];
  private _currentTurn = 0;

  /** Bound for ergonomic use as a callback. */
  readonly record = (result: SubagentResultLike): SpawnDistillation => {
    const d = computeSpawnDistillation(result);
    this._spawns.push(d);
    while (this._byTurn.length <= this._currentTurn) this._byTurn.push([]);
    this._byTurn[this._currentTurn]!.push(d);
    return d;
  };

  /** Mark the start of a new parent turn so subsequent records group into a new bucket — call from the parent loop when its turn counter advances. */
  startTurn(turn: number): void {
    if (turn < 0) return;
    this._currentTurn = turn;
  }

  get spawns(): readonly SpawnDistillation[] {
    return this._spawns;
  }

  get spawnsByTurn(): ReadonlyArray<ReadonlyArray<SpawnDistillation>> {
    return this._byTurn;
  }

  get summary(): SubagentSessionSummary {
    return summarizeSubagentSession(this._spawns);
  }

  stormCount(threshold: number = DEFAULT_SPAWN_STORM_THRESHOLD): number {
    return countSpawnStorms(this._byTurn, threshold);
  }
}
