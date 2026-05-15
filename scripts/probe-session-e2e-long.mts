/**
 * Round 4 (long) — 12-turn FLAT-vs-SUB session, 3 repeats per mode.
 *
 * Extends Round 4. Round 4 saw SUB crossing per-turn cheap by turn 6
 * but didn't actually reach cumulative crossover. This probe:
 *   - doubles the script length to 12 turns
 *   - runs each mode 3 times so model spawn-decision variance shows up
 *   - reports per-run cumulative crossover (if any) + median per-turn cost
 *
 * Cost budget: ~$0.30. Run time: 10-15 min.
 *
 * Run: npx tsx scripts/probe-session-e2e-long.mts
 */

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { DeepSeekClient } from "../src/client.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
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
const REPEATS = Number.parseInt(process.env.PROBE_REPEATS ?? "3", 10);

function registerReadTools(reg: ToolRegistry): void {
  reg.register({
    name: "list_dir",
    description: "List entries under ./src. One path per line, prefixed 'd ' or 'f '.",
    parameters: {
      type: "object",
      properties: { path: { type: "string", description: "Relative to ./src; '' for root." } },
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
      properties: { path: { type: "string", description: "Path relative to ./src." } },
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

interface SpawnTrace {
  task: string;
  completionTokens: number;
  outputTokens: number;
  savings: number;
  costUsd: number;
}

function registerSpawnSubagentWithTrace(reg: ToolRegistry, client: DeepSeekClient, traces: SpawnTrace[]): void {
  reg.register({
    name: "spawn_subagent",
    parallelSafe: true,
    description:
      "Spawn an isolated sub-agent for a self-contained read-heavy subtask (multi-file investigation, summarization, broad search). Prefer direct tools for single reads. The sub-agent inherits your tools but runs in its own log; only its final answer comes back. Cap: 8 tool iters per spawn.",
    parameters: {
      type: "object",
      properties: {
        task: { type: "string", description: "Self-contained subtask — sub-agent has none of your context." },
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
          "You are a read-only investigator. Use list_dir / read_file as needed. When done, return the distilled answer in the format the parent requested — no preamble, no closing.",
        task,
        model: MODEL,
        maxToolIters: 8,
      });
      const outputTokens = countTokens(result.output);
      traces.push({
        task: task.slice(0, 80),
        completionTokens: result.usage.completionTokens,
        outputTokens,
        savings: Math.max(0, result.usage.completionTokens - outputTokens),
        costUsd: result.costUsd,
      });
      return result.output || "[subagent returned empty]";
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
  cacheHit: number;
  cacheMiss: number;
  completionTokens: number;
  costUsd: number;
  cumulativeCost: number;
  spawnsThisTurn: number;
  spawnCostThisTurn: number;
  spawnSavingsThisTurn: number;
}

async function runOnce(
  mode: "FLAT" | "SUB",
  client: DeepSeekClient,
  runIdx: number,
): Promise<{ rows: TurnRow[]; traces: SpawnTrace[]; elapsedMs: number }> {
  const reg = new ToolRegistry();
  registerReadTools(reg);
  const traces: SpawnTrace[] = [];
  if (mode === "SUB") registerSpawnSubagentWithTrace(reg, client, traces);

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
  let traceCountBefore = 0;
  const t0 = Date.now();

  for (let i = 0; i < USER_SCRIPT.length; i++) {
    const userMsg = USER_SCRIPT[i]!;
    const turnsBefore = loop.stats.turns.length;
    try {
      for await (const ev of loop.step(userMsg)) {
        if (ev.role === "error") throw new Error(ev.error ?? "loop error");
      }
    } catch (err) {
      console.error(`  [${mode}#${runIdx} turn ${i + 1}] error: ${(err as Error).message}`);
    }
    const newTurns = loop.stats.turns.slice(turnsBefore);
    const promptTokens = newTurns.reduce((s, t) => s + t.usage.promptTokens, 0);
    const cacheHit = newTurns.reduce((s, t) => s + t.usage.promptCacheHitTokens, 0);
    const cacheMiss = newTurns.reduce((s, t) => s + t.usage.promptCacheMissTokens, 0);
    const completionTokens = newTurns.reduce((s, t) => s + t.usage.completionTokens, 0);
    const turnCost = newTurns.reduce((s, t) => s + t.cost, 0);
    const tracesThisTurn = traces.slice(traceCountBefore);
    traceCountBefore = traces.length;
    const spawnsThisTurn = tracesThisTurn.length;
    const spawnCostThisTurn = tracesThisTurn.reduce((s, t) => s + t.costUsd, 0);
    const spawnSavingsThisTurn = tracesThisTurn.reduce((s, t) => s + t.savings, 0);
    const totalTurnCost = turnCost + (mode === "SUB" ? spawnCostThisTurn : 0);
    cumulativeCost += totalTurnCost;
    rows.push({
      userTurn: i + 1,
      modelCalls: newTurns.length,
      promptTokens,
      cacheHit,
      cacheMiss,
      completionTokens,
      costUsd: totalTurnCost,
      cumulativeCost,
      spawnsThisTurn,
      spawnCostThisTurn,
      spawnSavingsThisTurn,
    });
  }
  return { rows, traces, elapsedMs: Date.now() - t0 };
}

interface Run {
  mode: "FLAT" | "SUB";
  runIdx: number;
  rows: TurnRow[];
  traces: SpawnTrace[];
  elapsedMs: number;
}

function median(xs: number[]): number {
  const s = [...xs].sort((a, b) => a - b);
  const n = s.length;
  if (n === 0) return 0;
  return n % 2 === 1 ? s[Math.floor(n / 2)]! : (s[n / 2 - 1]! + s[n / 2]!) / 2;
}

function findCrossover(flatRuns: Run[], subRuns: Run[]): { perRun: (number | null)[]; medianCrossover: number | null } {
  const perRun: (number | null)[] = [];
  for (let r = 0; r < subRuns.length; r++) {
    const subRows = subRuns[r]!.rows;
    const flatRows = flatRuns[Math.min(r, flatRuns.length - 1)]!.rows;
    let cross: number | null = null;
    for (let i = 0; i < Math.min(subRows.length, flatRows.length); i++) {
      if (subRows[i]!.cumulativeCost <= flatRows[i]!.cumulativeCost && i > 0) {
        cross = i + 1;
        break;
      }
    }
    perRun.push(cross);
  }
  const numericCrosses = perRun.filter((x): x is number => x !== null);
  return { perRun, medianCrossover: numericCrosses.length > 0 ? median(numericCrosses) : null };
}

async function main(): Promise<void> {
  if (!process.env.DEEPSEEK_API_KEY) {
    console.error("DEEPSEEK_API_KEY missing — populate .env first.");
    process.exit(1);
  }
  console.log(`probe-session-e2e-long  model=${MODEL}  turns=${USER_SCRIPT.length}  repeats=${REPEATS}`);
  const client = new DeepSeekClient();

  const flatRuns: Run[] = [];
  const subRuns: Run[] = [];

  for (let r = 0; r < REPEATS; r++) {
    console.log(`\n=== FLAT run ${r + 1}/${REPEATS} ===`);
    const flat = await runOnce("FLAT", client, r + 1);
    flatRuns.push({ mode: "FLAT", runIdx: r + 1, ...flat });
    const flatTotal = flat.rows[flat.rows.length - 1]!.cumulativeCost;
    console.log(`  FLAT#${r + 1} total $${flatTotal.toFixed(6)}  elapsed ${(flat.elapsedMs / 1000).toFixed(1)}s`);
    await new Promise((res) => setTimeout(res, 2000));

    console.log(`\n=== SUB run ${r + 1}/${REPEATS} ===`);
    const sub = await runOnce("SUB", client, r + 1);
    subRuns.push({ mode: "SUB", runIdx: r + 1, ...sub });
    const subTotal = sub.rows[sub.rows.length - 1]!.cumulativeCost;
    const subSpawns = sub.traces.length;
    console.log(`  SUB#${r + 1} total $${subTotal.toFixed(6)}  spawns=${subSpawns}  elapsed ${(sub.elapsedMs / 1000).toFixed(1)}s`);
    await new Promise((res) => setTimeout(res, 2000));
  }

  // Per-turn aggregates.
  console.log("\n=== Per-turn medians across runs ===");
  console.log("turn | FLAT prompt | SUB prompt | compression | FLAT cum $ | SUB cum $ | SUB-FLAT");
  const turnCount = USER_SCRIPT.length;
  for (let i = 0; i < turnCount; i++) {
    const flatPrompts = flatRuns.map((r) => r.rows[i]?.promptTokens ?? 0);
    const subPrompts = subRuns.map((r) => r.rows[i]?.promptTokens ?? 0);
    const flatCums = flatRuns.map((r) => r.rows[i]?.cumulativeCost ?? 0);
    const subCums = subRuns.map((r) => r.rows[i]?.cumulativeCost ?? 0);
    const fP = median(flatPrompts);
    const sP = median(subPrompts);
    const fC = median(flatCums);
    const sC = median(subCums);
    const comp = fP > 0 ? sP / fP : 1;
    const delta = sC - fC;
    console.log(
      `  ${(i + 1).toString().padStart(2)} | ${fP.toFixed(0).padStart(11)} | ${sP.toFixed(0).padStart(10)} |   ${(comp * 100).toFixed(1)}%   | $${fC.toFixed(6)} | $${sC.toFixed(6)} | ${delta >= 0 ? "+" : ""}${delta.toFixed(6)}`,
    );
  }

  // Per-run totals + crossover.
  console.log("\n=== Per-run totals ===");
  for (let r = 0; r < REPEATS; r++) {
    const f = flatRuns[r]!;
    const s = subRuns[r]!;
    const fT = f.rows[f.rows.length - 1]!.cumulativeCost;
    const sT = s.rows[s.rows.length - 1]!.cumulativeCost;
    const spawns = s.traces.length;
    const savings = s.traces.reduce((a, t) => a + t.savings, 0);
    const spawnCost = s.traces.reduce((a, t) => a + t.costUsd, 0);
    console.log(
      `  run ${r + 1}: FLAT $${fT.toFixed(6)}  SUB $${sT.toFixed(6)}  delta ${((sT - fT) / fT * 100).toFixed(1)}%  spawns=${spawns} spawnCost=$${spawnCost.toFixed(6)} savings=${savings}tok`,
    );
  }

  const { perRun: crosses, medianCrossover } = findCrossover(flatRuns, subRuns);
  console.log(`\nCumulative crossover per SUB run: ${crosses.map((x) => x ?? "never").join(", ")}`);
  console.log(`Median crossover turn: ${medianCrossover ?? "never within 12 turns"}`);

  const flatMedianTotal = median(flatRuns.map((r) => r.rows[r.rows.length - 1]!.cumulativeCost));
  const subMedianTotal = median(subRuns.map((r) => r.rows[r.rows.length - 1]!.cumulativeCost));
  console.log(`\nMedian totals: FLAT $${flatMedianTotal.toFixed(6)}  SUB $${subMedianTotal.toFixed(6)}  delta ${((subMedianTotal - flatMedianTotal) / flatMedianTotal * 100).toFixed(1)}%`);

  // Tail per-turn cost (last 3 turns) — the slope that predicts future savings.
  const tailFlat = median(
    flatRuns.flatMap((r) => r.rows.slice(-3).map((row) => row.costUsd)),
  );
  const tailSub = median(
    subRuns.flatMap((r) => r.rows.slice(-3).map((row) => row.costUsd)),
  );
  console.log(`Tail (last 3) median per-turn cost: FLAT $${tailFlat.toFixed(6)}  SUB $${tailSub.toFixed(6)}`);
  if (tailFlat > tailSub) {
    const turnsToRecoverDeficit = (subMedianTotal - flatMedianTotal) / (tailFlat - tailSub);
    console.log(`Per-turn delta at tail: SUB cheaper by $${(tailFlat - tailSub).toFixed(6)}/turn`);
    console.log(`Empirical extrapolation: SUB recovers in ${turnsToRecoverDeficit.toFixed(1)} additional turns past turn ${turnCount}`);
  }

  console.log("\nJSON:");
  console.log(JSON.stringify({
    model: MODEL,
    turns: turnCount,
    repeats: REPEATS,
    flatRuns: flatRuns.map((r) => ({
      runIdx: r.runIdx,
      total: r.rows[r.rows.length - 1]!.cumulativeCost,
      rows: r.rows,
    })),
    subRuns: subRuns.map((r) => ({
      runIdx: r.runIdx,
      total: r.rows[r.rows.length - 1]!.cumulativeCost,
      spawns: r.traces.length,
      spawnCost: r.traces.reduce((a, t) => a + t.costUsd, 0),
      spawnSavings: r.traces.reduce((a, t) => a + t.savings, 0),
      traces: r.traces,
      rows: r.rows,
    })),
    crossovers: crosses,
    medianCrossover,
    flatMedianTotal,
    subMedianTotal,
    tailFlat,
    tailSub,
  }, null, 2));
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
