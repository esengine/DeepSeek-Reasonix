import { basename } from "node:path";
import { loadSession, simulate, type RunResult, type ThresholdConfig } from "./simulator.js";

const BASELINE: ThresholdConfig = {
  ctxMax: 1_000_000,
  foldThreshold: 0.5,
  tailFraction: 0.2,
  aggressiveThreshold: 0.7,
  aggressiveTailFraction: 0.1,
  minSavingsFraction: 0.3,
  summaryRatio: 0.05,
};

interface NamedConfig {
  name: string;
  config: ThresholdConfig;
}

function configs(ctxMax: number): NamedConfig[] {
  const mk = (over: Partial<ThresholdConfig>): ThresholdConfig => ({ ...BASELINE, ...over, ctxMax });
  return [
    { name: "no-fold", config: mk({ foldThreshold: 999, aggressiveThreshold: 999 }) },
    { name: "current (50/70)", config: mk({ foldThreshold: 0.5, aggressiveThreshold: 0.7 }) },
    { name: "late (60/80)", config: mk({ foldThreshold: 0.6, aggressiveThreshold: 0.8 }) },
    { name: "very-late (75/90)", config: mk({ foldThreshold: 0.75, aggressiveThreshold: 0.9 }) },
    { name: "early (35/55)", config: mk({ foldThreshold: 0.35, aggressiveThreshold: 0.55 }) },
  ];
}

function parseArgs(argv: string[]): { sessions: string[]; ctxMax: number } {
  const sessions: string[] = [];
  let ctxMax = 1_000_000;
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i]!;
    if (a === "--ctx-max") ctxMax = Number(argv[++i]);
    else if (a === "--session") sessions.push(argv[++i]!);
    else if (!a.startsWith("--")) sessions.push(a);
  }
  return { sessions, ctxMax };
}

function fmt(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(2)}G`;
  if (n >= 1e6) return `${(n / 1e6).toFixed(2)}M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)}k`;
  return String(n);
}

function pct(n: number): string {
  return `${(n * 100).toFixed(1)}%`;
}

function report(label: string, rows: Array<{ name: string; result: RunResult }>): void {
  console.log(`\n=== ${label} ===`);
  const w = [22, 10, 10, 8, 10, 6, 10];
  const headers = ["config", "input", "cache_hit", "hit%", "miss", "folds", "peak"];
  console.log(headers.map((h, i) => h.padEnd(w[i]!)).join("│ "));
  console.log("─".repeat(w.reduce((a, b) => a + b + 2, 0)));
  for (const { name, result: r } of rows) {
    const peak = r.perTurn.reduce((m, t) => Math.max(m, t.ratio), 0);
    console.log(
      [
        name.padEnd(w[0]!),
        fmt(r.totalInputTokens).padEnd(w[1]!),
        fmt(r.totalCacheHitTokens).padEnd(w[2]!),
        pct(r.cacheHitRatio).padEnd(w[3]!),
        fmt(r.totalCacheMissTokens).padEnd(w[4]!),
        String(r.foldCount).padEnd(w[5]!),
        pct(peak).padEnd(w[6]!),
      ].join("│ "),
    );
  }
}

function main(): void {
  const { sessions, ctxMax } = parseArgs(process.argv.slice(2));
  if (sessions.length === 0) {
    console.error("usage: tsx run.ts [--ctx-max N] <session.jsonl> [more...]");
    process.exit(1);
  }
  for (const path of sessions) {
    const msgs = loadSession(path);
    const cfgs = configs(ctxMax);
    const rows = cfgs.map(({ name, config }) => ({ name, result: simulate(msgs, config, path) }));
    report(`${basename(path)}  (${msgs.length} msgs, ctxMax=${fmt(ctxMax)})`, rows);
  }
}

main();
