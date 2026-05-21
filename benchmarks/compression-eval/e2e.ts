import { spawnSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

interface RunSpec {
  name: string;
  foldThreshold: number;
  aggressiveThreshold: number;
}

interface RunSummary {
  name: string;
  repeat: number;
  turns: number;
  cacheHitRatio: number;
  costUsd: number;
  totalInputTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  foldCount: number;
  exitCode: number;
}

const RUNS: RunSpec[] = [
  { name: "no-fold", foldThreshold: 0.99, aggressiveThreshold: 0.99 },
  { name: "current_50_70", foldThreshold: 0.5, aggressiveThreshold: 0.7 },
  { name: "very_late_75_90", foldThreshold: 0.75, aggressiveThreshold: 0.9 },
  { name: "aggressive_90_95", foldThreshold: 0.9, aggressiveThreshold: 0.95 },
];

function parseArgs(argv: string[]): {
  taskFile: string;
  budget: number;
  outDir: string;
  only: string | null;
  fakeCtxMax: number | null;
  repeats: number;
} {
  let taskFile = "";
  let budget = 2.0;
  let outDir = "";
  let only: string | null = null;
  let fakeCtxMax: number | null = null;
  let repeats = 1;
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i]!;
    if (a === "--task-file") taskFile = argv[++i]!;
    else if (a === "--budget") budget = Number(argv[++i]);
    else if (a === "--out-dir") outDir = argv[++i]!;
    else if (a === "--only") only = argv[++i]!;
    else if (a === "--fake-ctxmax") fakeCtxMax = Number(argv[++i]);
    else if (a === "--repeats") repeats = Number(argv[++i]);
  }
  if (!taskFile) {
    console.error(
      "usage: tsx e2e.ts --task-file <p> [--budget N] [--out-dir <p>] [--only <n>] [--fake-ctxmax N] [--repeats N]",
    );
    process.exit(1);
  }
  if (!outDir) {
    const ts = new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19);
    outDir = `benchmarks/compression-eval/runs/${ts}`;
  }
  return { taskFile, budget, outDir, only, fakeCtxMax, repeats };
}

function loadTranscriptSummary(path: string): Partial<RunSummary> {
  let turns = 0;
  let inputTokens = 0;
  let cacheHit = 0;
  let cacheMiss = 0;
  let costUsd = 0;
  let foldCount = 0;
  const promptSeq: number[] = [];
  try {
    const lines = readFileSync(path, "utf8").split("\n");
    for (const ln of lines) {
      const t = ln.trim();
      if (!t) continue;
      let rec: {
        role?: string;
        prompt_tokens?: number;
        cache_hit_tokens?: number;
        cache_miss_tokens?: number;
        cost_usd?: number;
      };
      try {
        rec = JSON.parse(t);
      } catch {
        continue;
      }
      if (rec.role === "assistant_final") {
        turns++;
        const pt = rec.prompt_tokens ?? 0;
        inputTokens += pt;
        cacheHit += rec.cache_hit_tokens ?? 0;
        cacheMiss += rec.cache_miss_tokens ?? 0;
        costUsd += rec.cost_usd ?? 0;
        if (pt > 0) promptSeq.push(pt);
      } else if (rec.role === "fold_event") {
        foldCount++;
      }
    }
  } catch {
    /* file missing — caller will see exitCode and zeros */
  }
  if (foldCount === 0) {
    for (let i = 1; i < promptSeq.length; i++) {
      if (promptSeq[i]! < promptSeq[i - 1]! * 0.6) foldCount++;
    }
  }
  return {
    turns,
    totalInputTokens: inputTokens,
    cacheHitTokens: cacheHit,
    cacheMissTokens: cacheMiss,
    cacheHitRatio: inputTokens > 0 ? cacheHit / inputTokens : 0,
    costUsd,
    foldCount,
  };
}

function runOne(
  spec: RunSpec,
  task: string,
  budget: number,
  outDir: string,
  fakeCtxMax: number | null,
  repeat: number,
): RunSummary {
  const runName = repeat > 1 ? `${spec.name}_r${repeat}` : spec.name;
  const runDir = join(outDir, runName);
  mkdirSync(runDir, { recursive: true });
  const transcript = join(runDir, "transcript.jsonl");
  const logPath = join(runDir, "stdout.log");

  console.log(
    `\n>>> [${spec.name}] fold=${spec.foldThreshold} agg=${spec.aggressiveThreshold}`,
  );
  const tsxCli = join("node_modules", "tsx", "dist", "cli.mjs");
  const taskFilePath = join(runDir, "task.txt");
  writeFileSync(taskFilePath, task);
  const t0 = Date.now();
  const res = spawnSync(
    process.execPath,
    [
      tsxCli,
      "benchmarks/compression-eval/driver.ts",
      "--task-file",
      taskFilePath,
      "--transcript-out",
      transcript,
      "--budget",
      String(budget),
      ...(fakeCtxMax ? ["--fake-ctxmax", String(fakeCtxMax)] : []),
    ],
    {
      env: {
        ...process.env,
        REASONIX_FOLD_THRESHOLD: String(spec.foldThreshold),
        REASONIX_FOLD_AGGRESSIVE_THRESHOLD: String(spec.aggressiveThreshold),
      },
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
      shell: false,
      maxBuffer: 1024 * 1024 * 128,
    },
  );
  const exitCode = res.status ?? -1;
  const stdout = res.stdout ?? "";
  const stderr = res.stderr ?? "";
  const elapsedS = Math.round((Date.now() - t0) / 1000);
  writeFileSync(logPath, `${stdout}\n--- stderr ---\n${stderr}`);
  console.log(`    exit=${exitCode} elapsed=${elapsedS}s log=${logPath}`);

  const parsed = loadTranscriptSummary(transcript);
  return {
    name: spec.name,
    repeat,
    turns: parsed.turns ?? 0,
    cacheHitRatio: parsed.cacheHitRatio ?? 0,
    costUsd: parsed.costUsd ?? 0,
    totalInputTokens: parsed.totalInputTokens ?? 0,
    cacheHitTokens: parsed.cacheHitTokens ?? 0,
    cacheMissTokens: parsed.cacheMissTokens ?? 0,
    foldCount: parsed.foldCount ?? 0,
    exitCode,
  };
}

function pct(n: number): string {
  return `${(n * 100).toFixed(1)}%`;
}
function fmtTok(n: number): string {
  if (n >= 1e6) return `${(n / 1e6).toFixed(2)}M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)}k`;
  return String(n);
}

interface AggregateRow {
  name: string;
  n: number;
  meanCost: number;
  stdCost: number;
  meanMiss: number;
  stdMiss: number;
  meanHitPct: number;
  stdHitPct: number;
  meanTurns: number;
  meanFolds: number;
}

function meanStd(xs: number[]): [number, number] {
  if (xs.length === 0) return [0, 0];
  const m = xs.reduce((a, b) => a + b, 0) / xs.length;
  const v = xs.reduce((a, b) => a + (b - m) ** 2, 0) / xs.length;
  return [m, Math.sqrt(v)];
}

function aggregateByConfig(rows: RunSummary[]): AggregateRow[] {
  const groups = new Map<string, RunSummary[]>();
  for (const r of rows) {
    if (!groups.has(r.name)) groups.set(r.name, []);
    groups.get(r.name)!.push(r);
  }
  const result: AggregateRow[] = [];
  for (const [name, group] of groups) {
    const [meanCost, stdCost] = meanStd(group.map((r) => r.costUsd));
    const [meanMiss, stdMiss] = meanStd(group.map((r) => r.cacheMissTokens));
    const [meanHitPct, stdHitPct] = meanStd(group.map((r) => r.cacheHitRatio * 100));
    const [meanTurns] = meanStd(group.map((r) => r.turns));
    const [meanFolds] = meanStd(group.map((r) => r.foldCount));
    result.push({
      name,
      n: group.length,
      meanCost,
      stdCost,
      meanMiss,
      stdMiss,
      meanHitPct,
      stdHitPct,
      meanTurns,
      meanFolds,
    });
  }
  return result;
}

function reportAggregate(rows: AggregateRow[]): string {
  const cols: Array<[string, (r: AggregateRow) => string]> = [
    ["config", (r) => r.name],
    ["n", (r) => String(r.n)],
    ["turns", (r) => r.meanTurns.toFixed(1)],
    ["folds", (r) => r.meanFolds.toFixed(1)],
    ["hit%", (r) => `${r.meanHitPct.toFixed(1)}±${r.stdHitPct.toFixed(1)}`],
    ["miss", (r) => `${fmtTok(r.meanMiss)}±${fmtTok(r.stdMiss)}`],
    ["cost$", (r) => `${r.meanCost.toFixed(4)}±${r.stdCost.toFixed(4)}`],
  ];
  const widths = cols.map(([h, f]) => Math.max(h.length, ...rows.map((r) => f(r).length)));
  const head = cols.map(([h], i) => h.padEnd(widths[i]!)).join("│ ");
  const sep = "─".repeat(widths.reduce((a, b) => a + b + 2, 0));
  const body = rows
    .map((r) => cols.map(([_, f], i) => f(r).padEnd(widths[i]!)).join("│ "))
    .join("\n");
  return `${head}\n${sep}\n${body}`;
}

function reportTable(rows: RunSummary[]): string {
  const cols: Array<[string, (r: RunSummary) => string]> = [
    ["config", (r) => r.name],
    ["turns", (r) => String(r.turns)],
    ["input", (r) => fmtTok(r.totalInputTokens)],
    ["hit", (r) => fmtTok(r.cacheHitTokens)],
    ["hit%", (r) => pct(r.cacheHitRatio)],
    ["miss", (r) => fmtTok(r.cacheMissTokens)],
    ["folds", (r) => String(r.foldCount)],
    ["cost", (r) => `$${r.costUsd.toFixed(4)}`],
    ["exit", (r) => String(r.exitCode)],
  ];
  const widths = cols.map(([h, f]) => Math.max(h.length, ...rows.map((r) => f(r).length)));
  const head = cols.map(([h], i) => h.padEnd(widths[i]!)).join("│ ");
  const sep = "─".repeat(widths.reduce((a, b) => a + b + 2, 0));
  const body = rows
    .map((r) => cols.map(([_, f], i) => f(r).padEnd(widths[i]!)).join("│ "))
    .join("\n");
  return `${head}\n${sep}\n${body}`;
}

function main(): void {
  const { taskFile, budget, outDir, only, fakeCtxMax, repeats } = parseArgs(process.argv.slice(2));
  const task = readFileSync(taskFile, "utf8").trim();
  mkdirSync(outDir, { recursive: true });
  const specs = only ? RUNS.filter((r) => r.name === only) : RUNS;
  if (specs.length === 0) {
    console.error(`no config matches --only=${only}; available: ${RUNS.map((r) => r.name).join(", ")}`);
    process.exit(1);
  }
  console.log(`task: ${taskFile} (${task.length} chars)`);
  console.log(`out:  ${outDir}`);
  console.log(`budget per run: $${budget}`);
  console.log(`configs: ${specs.map((s) => s.name).join(", ")}`);
  console.log(`repeats: ${repeats}`);
  if (fakeCtxMax) console.log(`fake ctxMax: ${fakeCtxMax}`);

  const summaries: RunSummary[] = [];
  for (let r = 1; r <= repeats; r++) {
    for (const spec of specs) {
      summaries.push(runOne(spec, task, budget, outDir, fakeCtxMax, r));
    }
  }

  const table = reportTable(summaries);
  console.log(`\n=== Raw runs ===\n${table}`);
  if (repeats > 1) {
    const agg = aggregateByConfig(summaries);
    const aggTable = reportAggregate(agg);
    console.log(`\n=== Aggregate (mean ± std) ===\n${aggTable}`);
    writeFileSync(join(outDir, "aggregate.txt"), aggTable);
  }
  writeFileSync(join(outDir, "summary.json"), JSON.stringify(summaries, null, 2));
  writeFileSync(join(outDir, "summary.txt"), table);
  console.log(`\nsaved: ${join(outDir, "summary.json")}`);
}

main();
