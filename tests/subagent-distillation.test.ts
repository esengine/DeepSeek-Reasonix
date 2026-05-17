import { describe, expect, it } from "vitest";
import { Usage } from "../src/client.js";
import {
  DEFAULT_SPAWN_STORM_THRESHOLD,
  type SpawnDistillation,
  SubagentTelemetry,
  computeSpawnDistillation,
  countSpawnStorms,
  summarizeSubagentSession,
} from "../src/telemetry/subagent-distillation.js";
import { countTokens } from "../src/tokenizer.js";
import type { SubagentResult } from "../src/tools/subagent.js";

function fakeResult(opts: {
  output: string;
  completionTokens?: number;
  costUsd?: number;
}): SubagentResult {
  const usage = new Usage();
  usage.completionTokens = opts.completionTokens ?? 0;
  return {
    success: true,
    output: opts.output,
    turns: 1,
    toolIters: 0,
    elapsedMs: 0,
    costUsd: opts.costUsd ?? 0,
    model: "deepseek-chat",
    usage,
  };
}

describe("computeSpawnDistillation", () => {
  it("computes the read-heavy strength case: heavy completion, short output", () => {
    const d = computeSpawnDistillation(
      fakeResult({ output: "module A: counter; module B: stack.", completionTokens: 1000 }),
    );
    expect(d.completionTokens).toBe(1000);
    expect(d.outputTokens).toBeGreaterThan(0);
    expect(d.outputTokens).toBeLessThan(50);
    expect(d.savingsTokens).toBeGreaterThan(900);
    expect(d.compressionRatio).toBeLessThan(0.1);
    expect(d.hasOutput).toBe(true);
  });

  it("computes the write-heavy near-1 case", () => {
    const code = "```ts\nfunction add(a:number,b:number){return a+b;}\n```";
    const d = computeSpawnDistillation(
      fakeResult({ output: code, completionTokens: countTokens(code) }),
    );
    expect(d.compressionRatio).toBe(1);
    expect(d.savingsTokens).toBe(0);
  });

  it("flags empty output as not useful", () => {
    const d = computeSpawnDistillation(fakeResult({ output: "", completionTokens: 500 }));
    expect(d.hasOutput).toBe(false);
    expect(d.outputTokens).toBe(0);
    expect(d.savingsTokens).toBe(500);
  });

  it("flags whitespace-only output as not useful", () => {
    const d = computeSpawnDistillation(fakeResult({ output: "   \n\t\n", completionTokens: 200 }));
    expect(d.hasOutput).toBe(false);
  });

  it("clamps savings to 0 when output is somehow larger than completion (passthrough)", () => {
    const d = computeSpawnDistillation(
      fakeResult({ output: "x".repeat(5000), completionTokens: 10 }),
    );
    expect(d.savingsTokens).toBe(0);
    expect(d.compressionRatio).toBeGreaterThan(1);
  });

  it("defaults compressionRatio to 1 when completionTokens is 0", () => {
    const d = computeSpawnDistillation(fakeResult({ output: "x", completionTokens: 0 }));
    expect(d.compressionRatio).toBe(1);
  });
});

describe("summarizeSubagentSession", () => {
  it("returns a sane zero shape on empty input", () => {
    const s = summarizeSubagentSession([]);
    expect(s.spawnCount).toBe(0);
    expect(s.successRate).toBe(0);
    expect(s.aggregateCompressionRatio).toBe(1);
    expect(s.totalSavingsTokens).toBe(0);
  });

  it("aggregates per-spawn distillations into a session summary", () => {
    const spawns: SpawnDistillation[] = [
      mkSpawn({ completion: 1000, output: 50, cost: 0.003, useful: true }),
      mkSpawn({ completion: 800, output: 0, cost: 0.002, useful: false }),
      mkSpawn({ completion: 600, output: 30, cost: 0.001, useful: true }),
    ];
    const s = summarizeSubagentSession(spawns);
    expect(s.spawnCount).toBe(3);
    expect(s.usefulSpawnCount).toBe(2);
    expect(s.successRate).toBeCloseTo(2 / 3);
    expect(s.totalCompletionTokens).toBe(2400);
    expect(s.totalOutputTokens).toBe(80);
    expect(s.totalSavingsTokens).toBe(950 + 800 + 570);
    expect(s.aggregateCompressionRatio).toBeCloseTo(80 / 2400);
    expect(s.totalCostUsd).toBeCloseTo(0.006);
  });

  it("weights compression by completion tokens, not naive mean", () => {
    // Naive mean of (0.1, 0.9) would be 0.5. Weighted by completion (100, 900)
    // should land near 0.82.
    const spawns: SpawnDistillation[] = [
      mkSpawn({ completion: 100, output: 10, cost: 0, useful: true }),
      mkSpawn({ completion: 900, output: 810, cost: 0, useful: true }),
    ];
    const s = summarizeSubagentSession(spawns);
    expect(s.aggregateCompressionRatio).toBeCloseTo((10 + 810) / 1000);
  });
});

describe("countSpawnStorms", () => {
  it("counts turns where spawn count ≥ threshold", () => {
    const turns: SpawnDistillation[][] = [
      [],
      [mkSpawn({ completion: 100, output: 10, cost: 0, useful: true })],
      [
        mkSpawn({ completion: 100, output: 10, cost: 0, useful: true }),
        mkSpawn({ completion: 100, output: 10, cost: 0, useful: true }),
        mkSpawn({ completion: 100, output: 10, cost: 0, useful: true }),
      ],
      [
        mkSpawn({ completion: 100, output: 10, cost: 0, useful: true }),
        mkSpawn({ completion: 100, output: 10, cost: 0, useful: true }),
        mkSpawn({ completion: 100, output: 10, cost: 0, useful: true }),
        mkSpawn({ completion: 100, output: 10, cost: 0, useful: true }),
      ],
    ];
    expect(countSpawnStorms(turns)).toBe(2);
    expect(countSpawnStorms(turns, 4)).toBe(1);
    expect(countSpawnStorms([])).toBe(0);
  });

  it("uses DEFAULT_SPAWN_STORM_THRESHOLD = 3", () => {
    expect(DEFAULT_SPAWN_STORM_THRESHOLD).toBe(3);
  });
});

describe("SubagentTelemetry", () => {
  it("starts empty and reports a zero summary", () => {
    const t = new SubagentTelemetry();
    expect(t.spawns).toHaveLength(0);
    expect(t.summary.spawnCount).toBe(0);
    expect(t.stormCount()).toBe(0);
  });

  it("record() captures one distillation per call and returns it", () => {
    const t = new SubagentTelemetry();
    const d = t.record(fakeResult({ output: "ok", completionTokens: 100, costUsd: 0.001 }));
    expect(d.completionTokens).toBe(100);
    expect(d.hasOutput).toBe(true);
    expect(t.spawns).toHaveLength(1);
    expect(t.summary.totalCompletionTokens).toBe(100);
  });

  it("groups spawns into turn buckets via startTurn(), enabling storm counting", () => {
    const t = new SubagentTelemetry();
    t.record(fakeResult({ output: "a", completionTokens: 50 }));
    t.startTurn(1);
    t.record(fakeResult({ output: "b", completionTokens: 60 }));
    t.record(fakeResult({ output: "c", completionTokens: 70 }));
    t.record(fakeResult({ output: "d", completionTokens: 80 }));
    expect(t.spawnsByTurn).toHaveLength(2);
    expect(t.spawnsByTurn[0]).toHaveLength(1);
    expect(t.spawnsByTurn[1]).toHaveLength(3);
    expect(t.stormCount()).toBe(1);
    expect(t.stormCount(4)).toBe(0);
  });

  it("record is bound so it can be passed as a callback without losing `this`", () => {
    const t = new SubagentTelemetry();
    const handler = t.record;
    handler(fakeResult({ output: "x", completionTokens: 10 }));
    expect(t.spawns).toHaveLength(1);
  });

  it("summary reflects updates after each record", () => {
    const t = new SubagentTelemetry();
    t.record(fakeResult({ output: "first", completionTokens: 200, costUsd: 0.002 }));
    expect(t.summary.spawnCount).toBe(1);
    t.record(fakeResult({ output: "", completionTokens: 300, costUsd: 0.003 }));
    expect(t.summary.spawnCount).toBe(2);
    expect(t.summary.usefulSpawnCount).toBe(1);
    expect(t.summary.totalCostUsd).toBeCloseTo(0.005);
  });
});

function mkSpawn(opts: {
  completion: number;
  output: number;
  cost: number;
  useful: boolean;
}): SpawnDistillation {
  return {
    completionTokens: opts.completion,
    outputTokens: opts.output,
    savingsTokens: Math.max(0, opts.completion - opts.output),
    compressionRatio: opts.completion > 0 ? opts.output / opts.completion : 1,
    hasOutput: opts.useful,
    costUsd: opts.cost,
  };
}
