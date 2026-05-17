/**
 * Diagnose why ~46% of spawns in Round 4-long returned empty output.
 *
 * Hypothesis (from reading src/tools/subagent.ts):
 *   - maxToolIters cap fires `onIterBudgetExhausted: "pause"` →
 *     SubagentResult.paused=true, success=true, output=""
 *   - The probe's wrapper treated empty output as "free savings" because
 *     it only inspected output length, not paused.
 *
 * This probe re-runs three of Round 4-long's failed-spawn tasks at
 * three different maxToolIters values (8, 16, 32) and reports:
 *   - paused?
 *   - errorMessage?
 *   - toolIters used (vs cap)
 *   - output length
 *   - cost
 *
 * Run: npx tsx scripts/probe-empty-output-diagnosis.mts
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
      return text.length <= MAX_READ_CHARS ? text : `${text.slice(0, MAX_READ_CHARS)}\n[truncated]`;
    },
  });
  return reg;
}

const TASKS = [
  {
    id: "loop-prefix-wiring",
    task:
      "In src/loop.ts, find how `this.prefix` (an ImmutablePrefix instance) is wired into a turn — specifically, find which method builds the message array sent to the API and how prefix is used there. Return: method name, line number range, and 1-sentence summary. ≤80 words total.",
  },
  {
    id: "find-cachefirstloop-construct",
    task:
      "Search the src/ directory for files that construct `new CacheFirstLoop`. List up to 5 such files with the line number of construction. Return ONLY a bulleted list, ≤8 bullets.",
  },
  {
    id: "repair-usage",
    task:
      "In src/loop.ts, find all uses of `this.repair.` Return up to 6 lines with their line numbers and a 1-sentence purpose each. Bullets only.",
  },
];

const ITER_BUDGETS = [8, 16, 32];

interface DiagResult {
  task: string;
  maxIters: number;
  paused: boolean;
  success: boolean;
  hadError: boolean;
  errorMessage?: string;
  toolItersUsed: number;
  childTurns: number;
  outputLength: number;
  outputTokens: number;
  completionTokens: number;
  costUsd: number;
  elapsedMs: number;
  outputPreview: string;
}

async function diagnose(client: DeepSeekClient, taskId: string, taskText: string, maxIters: number): Promise<DiagResult> {
  const reg = buildRegistry();
  const t0 = Date.now();
  const result = await spawnSubagent({
    client,
    parentRegistry: reg,
    system:
      "You are a read-only investigator. Use list_dir / read_file. When done, return the distilled answer in the format requested — no preamble, no closing.",
    task: taskText,
    model: MODEL,
    maxToolIters: maxIters,
  });
  return {
    task: taskId,
    maxIters,
    paused: result.paused ?? false,
    success: result.success && result.output.trim().length > 0,
    hadError: result.error !== undefined,
    errorMessage: result.error,
    toolItersUsed: result.toolIters,
    childTurns: result.turns,
    outputLength: result.output.length,
    outputTokens: countTokens(result.output),
    completionTokens: result.usage.completionTokens,
    costUsd: result.costUsd,
    elapsedMs: Date.now() - t0,
    outputPreview: result.output.replace(/\s+/g, " ").slice(0, 140),
  };
}

async function main(): Promise<void> {
  if (!process.env.DEEPSEEK_API_KEY) {
    console.error("DEEPSEEK_API_KEY missing");
    process.exit(1);
  }
  console.log(`probe-empty-output-diagnosis  model=${MODEL}`);
  console.log(`tasks: ${TASKS.length}  budgets: ${ITER_BUDGETS.join(",")}`);

  const client = new DeepSeekClient();
  const results: DiagResult[] = [];

  for (const task of TASKS) {
    for (const cap of ITER_BUDGETS) {
      console.log(`\n--- ${task.id} @ maxIters=${cap} ---`);
      const r = await diagnose(client, task.id, task.task, cap);
      results.push(r);
      console.log(
        `  paused=${r.paused}  success=${r.success}  error=${r.hadError ? `"${r.errorMessage}"` : "no"}  iters=${r.toolItersUsed}/${cap}  childTurns=${r.childTurns}  out=${r.outputTokens}tok/${r.outputLength}ch  compl=${r.completionTokens}  $${r.costUsd.toFixed(6)}  ${(r.elapsedMs / 1000).toFixed(1)}s`,
      );
      if (r.outputLength > 0) console.log(`  preview: ${r.outputPreview}…`);
    }
  }

  console.log("\n=== Summary ===");
  console.log("task                       | budget |  success | paused | iters/cap | output");
  for (const r of results) {
    console.log(
      `  ${r.task.padEnd(26)} |  ${r.maxIters.toString().padStart(4)} |  ${r.success ? "yes" : "no "}    |  ${r.paused ? "yes" : "no "}   |  ${r.toolItersUsed}/${r.maxIters}      | ${r.outputTokens}tok`,
    );
  }

  // Per-task: budget at which it succeeded
  console.log("\n=== Per-task smallest successful budget ===");
  for (const t of TASKS) {
    const rs = results.filter((r) => r.task === t.id).sort((a, b) => a.maxIters - b.maxIters);
    const firstSuccess = rs.find((r) => r.success);
    if (firstSuccess) {
      console.log(`  ${t.id}: succeeded at maxIters=${firstSuccess.maxIters} (used ${firstSuccess.toolItersUsed}, cost $${firstSuccess.costUsd.toFixed(6)})`);
    } else {
      console.log(`  ${t.id}: NEVER succeeded across budgets [${rs.map((r) => r.maxIters).join(",")}]`);
    }
  }

  console.log("\nJSON:");
  console.log(JSON.stringify({ model: MODEL, results }, null, 2));
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
