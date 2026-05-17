/**
 * Round 6 — post-patch end-to-end. Same 12-turn × 3-repeat script as
 * Round 4-long, but wires the new SubagentTelemetry + new
 * forcedSummary routing in spawnSubagent.
 *
 * What we want to see vs Round 4-long:
 *   - useful-spawn rate ≫ 54% (Round 4-long's complement of the 46% empty rate)
 *   - some spawns reporting forcedSummary=true with non-empty output
 *     (previously their content was discarded to `error`)
 *   - per-turn distillation visible via telemetry.summary
 *
 * Run: npx tsx scripts/probe-session-e2e-post-patch.mts
 */

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { DeepSeekClient } from "../src/client.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
import {
  SubagentTelemetry,
  type SubagentSessionSummary,
} from "../src/telemetry/subagent-distillation.js";
import { ToolRegistry } from "../src/tools.js";
import {
  type SubagentResult,
  registerSubagentTool,
  spawnSubagent,
} from "../src/tools/subagent.js";

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
const REPEATS = Number.parseInt(process.env.PROBE_REPEATS ?? "3", 10);

function registerReadTools(reg: ToolRegistry): void {
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
}

/** Custom spawn tool that wires SubagentTelemetry AND captures the full SubagentResult shape per call (the registerSubagentTool callback only fires once, we want per-call inspection too). */
function registerSpawnSubagentWithTelemetry(
  reg: ToolRegistry,
  client: DeepSeekClient,
  telemetry: SubagentTelemetry,
  resultLog: SubagentResult[],
): void {
  reg.register({
    name: "spawn_subagent",
    parallelSafe: true,
    description:
      "Spawn an isolated sub-agent for a self-contained read-heavy subtask (multi-file investigation, summarization, broad search). Prefer direct tools for single reads. The sub-agent inherits your tools but runs in its own log; only its final answer comes back. Cap: 8 tool iters per spawn.",
    parameters: {
      type: "object",
      properties: {
        task: { type: "string", description: "Self-contained subtask." },
      },
      required: ["task"],
    },
    fn: async (args: { task?: string }) => {
      const task = (args.task ?? "").trim();
      if (!task) return JSON.stringify({ error: "spawn_subagent requires non-empty 'task'." });
      const childReg = new ToolRegistry();
      registerReadTools(childReg);
      const result = await spawnSubagent({
        client,
        parentRegistry: childReg,
        system:
          "You are a read-only investigator. Use list_dir / read_file as needed. When done, return the distilled answer — no preamble, no closing.",
        task,
        model: MODEL,
        maxToolIters: 8,
      });
      telemetry.record(result);
      resultLog.push(result);
      // Match formatSubagentResult shape so the parent agent sees the same surface
      // a real registerSubagentTool would emit.
      if (result.paused) {
        return JSON.stringify({
          success: false,
          paused: true,
          partial_summary: result.partialSummary,
          note: "spawn paused; not resumed in this probe",
        });
      }
      if (result.forcedSummary) {
        return JSON.stringify({
          success: false,
          partial: true,
          output: result.output,
          note: "spawn was force-summarized; output carries partial synthesis",
        });
      }
      if (!result.success) {
        return JSON.stringify({ success: false, error: result.error ?? "spawn failed" });
      }
      return JSON.stringify({ success: true, output: result.output });
    },
  });
}

const SYSTEM_PROMPT = (mode: "FLAT" | "SUB") =>
  `You are a code explorer. The codebase root is "./src" (passed as relative paths to list_dir / read_file). Use the tools to investigate, then answer concisely.

Rules:
- Match the format the user asks for (e.g. "3 bullets" = exactly 3 bullets, no preamble, no closing).
- Don't speculate beyond what the code shows.
- Be terse.${
    mode === "SUB"
      ? `

You also have spawn_subagent. Prefer direct list_dir/read_file for single-file or 1-2-step lookups; spawn for genuinely read-heavy investigations (3+ files, broad searches) where you only need the distilled answer.`
      : ""
  }`;

const USER_SCRIPT: string[] = [
  "Use list_dir with path='' to see the top-level src layout. Tell me the 5 most prominent top-level files or directories with a one-phrase guess at each one's purpose. Exactly 5 bullets.",
  "Pick three .ts files DIRECTLY under src/loop/ (not subdirs) and tell me what each one does in ≤12 words. Three bullets, nothing else.",
  "Pick three files from src/tools/ (the directory, skip subdirs) and tell me what each tool family is for in ≤12 words. Three bullets, nothing else.",
  "Pick three files from src/core/ and tell me what each does in ≤12 words. Three bullets.",
  "Pick three files from src/memory/ and tell me what each does in ≤12 words. Three bullets.",
  "Read src/tools.ts. In ≤3 sentences, how does the tool registry handle (a) registration and (b) dispatch? Just the answer.",
  "What is the role of the `repair` field on CacheFirstLoop? Read src/loop.ts as needed. One short paragraph.",
  "Find where `PauseGate` is defined and how it's used. One sentence each — definition file, primary purpose.",
  "Read src/memory/runtime.ts. What does ImmutablePrefix do and what fields does it expose? Two sentences.",
  "What's the difference in purpose between AppendOnlyLog and VolatileScratch? Find them in source and explain in ≤2 sentences.",
  "Look at how src/loop.ts wires ImmutablePrefix into a turn. Walk through the relevant lines in one short paragraph.",
  "Final synthesis: in exactly 5 bullets (≤15 words each), the core architectural pieces a new contributor must understand about how loop / tools / memory interact. No preamble.",
];

interface TurnRow {
  userTurn: number;
  modelCalls: number;
  promptTokens: number;
  completionTokens: number;
  costUsd: number;
  cumulativeCost: number;
  spawnsThisTurn: number;
}

async function runOnce(
  mode: "FLAT" | "SUB",
  client: DeepSeekClient,
  runIdx: number,
): Promise<{
  rows: TurnRow[];
  telemetry: SubagentTelemetry | null;
  resultLog: SubagentResult[];
  elapsedMs: number;
}> {
  const reg = new ToolRegistry();
  registerReadTools(reg);
  const telemetry = mode === "SUB" ? new SubagentTelemetry() : null;
  const resultLog: SubagentResult[] = [];
  if (mode === "SUB" && telemetry) {
    registerSpawnSubagentWithTelemetry(reg, client, telemetry, resultLog);
  }

  const loop = new CacheFirstLoop({
    client,
    prefix: new ImmutablePrefix({ system: SYSTEM_PROMPT(mode), toolSpecs: reg.specs() }),
    tools: reg,
    model: MODEL,
    stream: false,
    maxToolIters: 16,
  });

  const rows: TurnRow[] = [];
  let cumulativeCost = 0;
  const t0 = Date.now();

  for (let i = 0; i < USER_SCRIPT.length; i++) {
    telemetry?.startTurn(i);
    const userMsg = USER_SCRIPT[i]!;
    const turnsBefore = loop.stats.turns.length;
    const spawnsBefore = telemetry?.spawns.length ?? 0;
    try {
      for await (const ev of loop.step(userMsg)) {
        if (ev.role === "error") throw new Error(ev.error ?? "loop error");
      }
    } catch (err) {
      console.error(`  [${mode}#${runIdx} turn ${i + 1}] ${(err as Error).message}`);
    }
    const newTurns = loop.stats.turns.slice(turnsBefore);
    const promptTokens = newTurns.reduce((s, t) => s + t.usage.promptTokens, 0);
    const completionTokens = newTurns.reduce((s, t) => s + t.usage.completionTokens, 0);
    const turnCost = newTurns.reduce((s, t) => s + t.cost, 0);
    const spawnsThisTurn = (telemetry?.spawns.length ?? 0) - spawnsBefore;
    const spawnCostThisTurn = telemetry
      ? telemetry.spawns.slice(spawnsBefore).reduce((s, x) => s + x.costUsd, 0)
      : 0;
    const totalTurnCost = turnCost + spawnCostThisTurn;
    cumulativeCost += totalTurnCost;
    rows.push({
      userTurn: i + 1,
      modelCalls: newTurns.length,
      promptTokens,
      completionTokens,
      costUsd: totalTurnCost,
      cumulativeCost,
      spawnsThisTurn,
    });
  }
  return { rows, telemetry, resultLog, elapsedMs: Date.now() - t0 };
}

function median(xs: number[]): number {
  const s = [...xs].sort((a, b) => a - b);
  const n = s.length;
  if (n === 0) return 0;
  return n % 2 === 1 ? s[Math.floor(n / 2)]! : (s[n / 2 - 1]! + s[n / 2]!) / 2;
}

async function main(): Promise<void> {
  if (!process.env.DEEPSEEK_API_KEY) {
    console.error("DEEPSEEK_API_KEY missing");
    process.exit(1);
  }
  console.log(`probe-session-e2e-post-patch  model=${MODEL}  turns=${USER_SCRIPT.length}  repeats=${REPEATS}`);
  const client = new DeepSeekClient();

  const flatRuns: { rows: TurnRow[]; total: number; elapsedMs: number }[] = [];
  const subRuns: {
    rows: TurnRow[];
    total: number;
    elapsedMs: number;
    summary: SubagentSessionSummary;
    stormCount: number;
    forcedSummaryCount: number;
    pausedCount: number;
    usefulCount: number;
    resultLog: SubagentResult[];
  }[] = [];

  for (let r = 0; r < REPEATS; r++) {
    console.log(`\n=== FLAT run ${r + 1}/${REPEATS} ===`);
    const flat = await runOnce("FLAT", client, r + 1);
    const flatTotal = flat.rows[flat.rows.length - 1]!.cumulativeCost;
    flatRuns.push({ rows: flat.rows, total: flatTotal, elapsedMs: flat.elapsedMs });
    console.log(`  FLAT#${r + 1} total $${flatTotal.toFixed(6)}  ${(flat.elapsedMs / 1000).toFixed(1)}s`);
    await new Promise((res) => setTimeout(res, 2000));

    console.log(`\n=== SUB run ${r + 1}/${REPEATS} ===`);
    const sub = await runOnce("SUB", client, r + 1);
    const subTotal = sub.rows[sub.rows.length - 1]!.cumulativeCost;
    const tel = sub.telemetry!;
    const summary = tel.summary;
    const forcedSummaryCount = sub.resultLog.filter((x) => x.forcedSummary === true).length;
    const pausedCount = sub.resultLog.filter((x) => x.paused === true).length;
    const usefulCount = sub.resultLog.filter((x) => x.output.trim().length > 0).length;
    subRuns.push({
      rows: sub.rows,
      total: subTotal,
      elapsedMs: sub.elapsedMs,
      summary,
      stormCount: tel.stormCount(),
      forcedSummaryCount,
      pausedCount,
      usefulCount,
      resultLog: sub.resultLog,
    });
    console.log(
      `  SUB#${r + 1} total $${subTotal.toFixed(6)}  spawns=${summary.spawnCount}  useful=${usefulCount}  forcedSummary=${forcedSummaryCount}  paused=${pausedCount}  storms=${tel.stormCount()}  ${(sub.elapsedMs / 1000).toFixed(1)}s`,
    );
    console.log(
      `    summary: completion=${summary.totalCompletionTokens} output=${summary.totalOutputTokens} savings=${summary.totalSavingsTokens} compression=${(summary.aggregateCompressionRatio * 100).toFixed(1)}% successRate=${(summary.successRate * 100).toFixed(1)}%`,
    );
    await new Promise((res) => setTimeout(res, 2000));
  }

  console.log("\n=== Per-run totals ===");
  for (let r = 0; r < REPEATS; r++) {
    const f = flatRuns[r]!;
    const s = subRuns[r]!;
    const delta = ((s.total - f.total) / f.total) * 100;
    console.log(
      `  run ${r + 1}: FLAT $${f.total.toFixed(6)}  SUB $${s.total.toFixed(6)}  delta ${delta >= 0 ? "+" : ""}${delta.toFixed(1)}%  spawns=${s.summary.spawnCount} useful/spawns=${s.usefulCount}/${s.summary.spawnCount} forced=${s.forcedSummaryCount} paused=${s.pausedCount} storms=${s.stormCount}`,
    );
  }

  const flatMed = median(flatRuns.map((r) => r.total));
  const subMed = median(subRuns.map((r) => r.total));
  const usefulRate = subRuns.reduce((s, r) => s + r.usefulCount, 0) /
    Math.max(1, subRuns.reduce((s, r) => s + r.summary.spawnCount, 0));
  const forcedRate = subRuns.reduce((s, r) => s + r.forcedSummaryCount, 0) /
    Math.max(1, subRuns.reduce((s, r) => s + r.summary.spawnCount, 0));
  const pausedRate = subRuns.reduce((s, r) => s + r.pausedCount, 0) /
    Math.max(1, subRuns.reduce((s, r) => s + r.summary.spawnCount, 0));

  console.log("\n=== Aggregate vs Round 4-long ===");
  console.log(`  Median FLAT: $${flatMed.toFixed(6)}  Median SUB: $${subMed.toFixed(6)}  delta ${(((subMed - flatMed) / flatMed) * 100).toFixed(1)}%`);
  console.log(`  Spawn count (total across runs): ${subRuns.reduce((s, r) => s + r.summary.spawnCount, 0)}`);
  console.log(`  Useful-spawn rate (output non-empty): ${(usefulRate * 100).toFixed(1)}%   ← was 54% in Round 4-long`);
  console.log(`  Of which forcedSummary partial-answer: ${(forcedRate * 100).toFixed(1)}%   ← was 0% (content was in error field)`);
  console.log(`  Paused (recoverable, not used here): ${(pausedRate * 100).toFixed(1)}%`);
  console.log(`  Storm count (≥3 spawns/turn): ${subRuns.reduce((s, r) => s + r.stormCount, 0)}`);

  console.log("\nJSON:");
  console.log(
    JSON.stringify(
      {
        model: MODEL,
        turns: USER_SCRIPT.length,
        repeats: REPEATS,
        flatTotals: flatRuns.map((r) => r.total),
        subTotals: subRuns.map((r) => r.total),
        flatMedian: flatMed,
        subMedian: subMed,
        usefulRate,
        forcedRate,
        pausedRate,
        subRuns: subRuns.map((r) => ({
          total: r.total,
          summary: r.summary,
          stormCount: r.stormCount,
          forcedSummaryCount: r.forcedSummaryCount,
          pausedCount: r.pausedCount,
          usefulCount: r.usefulCount,
          spawnDetail: r.resultLog.map((res) => ({
            outputLen: res.output.length,
            success: res.success,
            paused: res.paused === true,
            forcedSummary: res.forcedSummary === true,
            completionTokens: res.usage.completionTokens,
            costUsd: res.costUsd,
          })),
        })),
      },
      null,
      2,
    ),
  );
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
