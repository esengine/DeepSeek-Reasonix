/**
 * Issue 3 — sweep `maxToolIters` across representative spawn workloads
 * to identify the right default for `spawn_subagent`.
 *
 * Current default: DEFAULT_PAUSE_EVERY = 16 (src/tools/subagent.ts).
 * Round 5 hint: 8 is too tight on common investigations; 16 succeeds
 * most of the time. Open: is 16 the knee, or is 24 / 32 better?
 *
 * Five tasks × five budgets = 25 spawns. ~$0.15, ~12 min.
 *
 * Run: npx tsx scripts/probe-budget-sweep.mts
 */

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { DeepSeekClient } from "../src/client.js";
import { ToolRegistry } from "../src/tools.js";
import { spawnSubagent } from "../src/tools/subagent.js";
import { countTokens } from "../src/tokenizer.js";

function loadDotenv(path: string): boolean {
  if (!existsSync(path)) return false;
  const txt = readFileSync(path, "utf8");
  for (const line of txt.split(/\r?\n/)) {
    const m = line.match(/^([A-Z_][A-Z0-9_]*)=(.*)$/);
    if (m && !process.env[m[1]!]) process.env[m[1]!] = m[2]!;
  }
  return true;
}
loadDotenv("./.env") || loadDotenv("./.env.testbak");

const ROOT = resolve("./src");
const MAX_READ_CHARS = 6000;
const MODEL = process.env.PROBE_MODEL ?? "deepseek-chat";

function buildRegistry(): ToolRegistry {
  const reg = new ToolRegistry();
  reg.register({
    name: "list_dir",
    description: "List entries under ./src.",
    parameters: {
      type: "object",
      properties: { path: { type: "string" } },
      required: ["path"],
    },
    fn: async (args: { path?: string }) => {
      const rel = (args.path ?? "").replace(/^[/\\]+/, "");
      const abs = join(ROOT, rel);
      if (!abs.startsWith(ROOT)) return "error: path escapes ./src";
      if (!existsSync(abs)) return `error: not found: ${rel}`;
      const out: string[] = [];
      for (const name of readdirSync(abs).sort()) {
        const st = statSync(join(abs, name));
        out.push(`${st.isDirectory() ? "d" : "f"} ${rel ? `${rel}/${name}` : name}`);
      }
      return out.join("\n") || "(empty)";
    },
  });
  reg.register({
    name: "read_file",
    description: `Read a file under ./src (first ${MAX_READ_CHARS} chars).`,
    parameters: {
      type: "object",
      properties: { path: { type: "string" } },
      required: ["path"],
    },
    fn: async (args: { path?: string }) => {
      const rel = (args.path ?? "").replace(/^[/\\]+/, "");
      const abs = join(ROOT, rel);
      if (!abs.startsWith(ROOT)) return "error: path escapes ./src";
      if (!existsSync(abs)) return `error: not found: ${rel}`;
      const text = readFileSync(abs, "utf8");
      return text.length <= MAX_READ_CHARS
        ? text
        : `${text.slice(0, MAX_READ_CHARS)}\n[truncated]`;
    },
  });
  return reg;
}

interface Task {
  id: string;
  difficulty: "easy" | "medium" | "hard" | "storm-prone";
  brief: string;
}

const TASKS: Task[] = [
  {
    id: "list-loop-dir",
    difficulty: "easy",
    brief: "List the immediate entries (files and subdirs) directly under src/loop/. Use list_dir on 'loop'. Return bullets of filenames only, no preamble.",
  },
  {
    id: "single-file-const",
    difficulty: "easy",
    brief: "Read src/tools/subagent.ts. What is the value of `DEFAULT_PAUSE_EVERY`? Return exactly one sentence stating the constant and its value.",
  },
  {
    id: "three-file-summary",
    difficulty: "medium",
    brief: "Read each of src/tools/filesystem.ts, src/tools/shell.ts, and src/tools/web.ts. Return exactly three bullets, one per file, each ≤12 words describing what the file does. No preamble.",
  },
  {
    id: "cross-file-search",
    difficulty: "hard",
    brief: "Find files directly under src/tools/ (not subdirs) that contain the literal string 'parallelSafe: true'. List the file paths only, one per bullet. No preamble.",
  },
  {
    id: "loop-prefix-wiring",
    difficulty: "storm-prone",
    brief: "In src/loop.ts, identify the method on CacheFirstLoop that builds the message array sent to the DeepSeek API and explain how `this.prefix` (an ImmutablePrefix) is used there. One short paragraph, no preamble.",
  },
];

const BUDGETS = [4, 8, 16, 24, 32];

interface CellResult {
  taskId: string;
  difficulty: Task["difficulty"];
  budget: number;
  success: boolean;
  paused: boolean;
  forcedSummary: boolean;
  toolItersUsed: number;
  outputTokens: number;
  outputLength: number;
  completionTokens: number;
  costUsd: number;
  elapsedMs: number;
  outputPreview: string;
}

async function runCell(client: DeepSeekClient, task: Task, budget: number): Promise<CellResult> {
  const reg = buildRegistry();
  const t0 = Date.now();
  const result = await spawnSubagent({
    client,
    parentRegistry: reg,
    system:
      "You are a read-only investigator. Use list_dir / read_file as needed. When done, return the distilled answer in the exact format requested — no preamble, no closing.",
    task: task.brief,
    model: MODEL,
    maxToolIters: budget,
  });
  return {
    taskId: task.id,
    difficulty: task.difficulty,
    budget,
    success: result.success && result.output.trim().length > 0,
    paused: result.paused === true,
    forcedSummary: result.forcedSummary === true,
    toolItersUsed: result.toolIters,
    outputTokens: countTokens(result.output),
    outputLength: result.output.length,
    completionTokens: result.usage.completionTokens,
    costUsd: result.costUsd,
    elapsedMs: Date.now() - t0,
    outputPreview: result.output.replace(/\s+/g, " ").slice(0, 100),
  };
}

async function main(): Promise<void> {
  if (!process.env.DEEPSEEK_API_KEY) {
    console.error("DEEPSEEK_API_KEY missing");
    process.exit(1);
  }
  console.log(`probe-budget-sweep  model=${MODEL}  tasks=${TASKS.length}  budgets=${BUDGETS.join(",")}`);

  const client = new DeepSeekClient();
  const cells: CellResult[] = [];

  for (const task of TASKS) {
    console.log(`\n## ${task.id} [${task.difficulty}]`);
    for (const budget of BUDGETS) {
      const r = await runCell(client, task, budget);
      cells.push(r);
      const flag = r.success ? "✓" : r.forcedSummary ? "F" : r.paused ? "P" : "✗";
      console.log(
        `  budget=${String(budget).padStart(2)}: ${flag} iters=${r.toolItersUsed}/${budget} out=${r.outputTokens}tok compl=${r.completionTokens} $${r.costUsd.toFixed(6)} ${(r.elapsedMs / 1000).toFixed(1)}s`,
      );
    }
  }

  // Per-budget aggregates
  console.log("\n=== Per-budget aggregates ===");
  console.log("budget | success | paused | forced | mean cost | mean output | tasks failed");
  for (const budget of BUDGETS) {
    const subset = cells.filter((c) => c.budget === budget);
    const successCount = subset.filter((c) => c.success).length;
    const pausedCount = subset.filter((c) => c.paused).length;
    const forcedCount = subset.filter((c) => c.forcedSummary).length;
    const meanCost = subset.reduce((s, c) => s + c.costUsd, 0) / subset.length;
    const meanOut = subset.reduce((s, c) => s + c.outputTokens, 0) / subset.length;
    const failed = subset.filter((c) => !c.success).map((c) => c.taskId);
    console.log(
      `   ${String(budget).padStart(2)}  |  ${successCount}/${subset.length}  |  ${pausedCount}  |   ${forcedCount}   | $${meanCost.toFixed(6)} | ${meanOut.toFixed(0)}tok | ${failed.join(", ") || "—"}`,
    );
  }

  // Per-task crossover: smallest budget at which each task succeeded
  console.log("\n=== Per-task: smallest successful budget ===");
  for (const task of TASKS) {
    const subset = cells.filter((c) => c.taskId === task.id).sort((a, b) => a.budget - b.budget);
    const firstSuccess = subset.find((c) => c.success);
    const minSuccessBudget = firstSuccess ? firstSuccess.budget : null;
    const status =
      minSuccessBudget !== null
        ? `succeeded at budget=${minSuccessBudget} (used ${firstSuccess!.toolItersUsed}, cost $${firstSuccess!.costUsd.toFixed(6)})`
        : `NEVER succeeded across budgets [${BUDGETS.join(",")}]`;
    console.log(`  ${task.id.padEnd(28)} [${task.difficulty.padEnd(11)}]: ${status}`);
  }

  // Knee identification
  console.log("\n=== Knee identification ===");
  const successByBudget = BUDGETS.map((b) => ({
    budget: b,
    successRate: cells.filter((c) => c.budget === b && c.success).length / TASKS.length,
    meanCost: cells.filter((c) => c.budget === b).reduce((s, c) => s + c.costUsd, 0) / TASKS.length,
  }));
  for (const row of successByBudget) {
    console.log(`  budget=${String(row.budget).padStart(2)}: ${(row.successRate * 100).toFixed(0)}% success, mean cost $${row.meanCost.toFixed(6)}`);
  }

  // Recommendation
  const target = 0.8;
  const knee = successByBudget.find((r) => r.successRate >= target);
  const currentDefault = successByBudget.find((r) => r.budget === 16);
  console.log(`\n  Target: success rate ≥ ${(target * 100).toFixed(0)}%`);
  console.log(
    `  Smallest budget that hits target: ${knee ? `${knee.budget} (${(knee.successRate * 100).toFixed(0)}% success, $${knee.meanCost.toFixed(6)} mean cost)` : `none in tested range`}`,
  );
  if (currentDefault) {
    console.log(
      `  Current default (DEFAULT_PAUSE_EVERY=16): ${(currentDefault.successRate * 100).toFixed(0)}% success, $${currentDefault.meanCost.toFixed(6)} mean cost`,
    );
  }

  console.log("\nJSON:");
  console.log(JSON.stringify({ model: MODEL, tasks: TASKS.length, budgets: BUDGETS, cells, successByBudget }, null, 2));
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
