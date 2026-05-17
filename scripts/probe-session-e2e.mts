/**
 * Round 4 — true end-to-end. Run the same multi-turn user script
 * twice against a live DeepSeek API:
 *
 *   FLAT — parent has list_dir + read_file only.
 *   SUB  — parent has the same tools plus spawn_subagent.
 *
 * Same model, same temperature defaults, same prompt fragments,
 * same user messages in the same order. The only variable is
 * whether the parent loop has access to spawn_subagent.
 *
 * Measurements per user turn (both modes):
 *   - prompt_tokens, cache_hit, cache_miss, completion_tokens
 *   - cumulative parent input cost
 *   - (SUB only) cumulative subagent spawn count + cost + savings
 *
 * Headline at the end:
 *   - SUB total session cost vs FLAT total session cost
 *   - crossover turn — first turn at which SUB's cumulative cost ≤ FLAT's
 *   - realized parent-log compression: how much smaller is SUB's
 *     turn-by-turn prompt than FLAT's at the same user-turn index
 *
 * Run: npx tsx scripts/probe-session-e2e.mts
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

function registerReadTools(reg: ToolRegistry): void {
  reg.register({
    name: "list_dir",
    description: "List entries under ./src. Returns one path per line, prefixed 'd ' or 'f '.",
    parameters: {
      type: "object",
      properties: { path: { type: "string", description: "Relative to ./src; use '' for root." } },
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

/** Manually-wired spawn tool — same behavior as registerSubagentTool but with a side channel for metrics. */
function registerSpawnSubagentWithTrace(
  reg: ToolRegistry,
  client: DeepSeekClient,
  traces: SpawnTrace[],
): void {
  reg.register({
    name: "spawn_subagent",
    parallelSafe: true,
    description:
      "Spawn an isolated sub-agent for a self-contained read-heavy subtask (multi-file investigation, summarization, broad search). Prefer direct tools for single reads. The sub-agent inherits your tools but runs in its own log; only its final answer comes back. Cap: 8 tool iters per spawn.",
    parameters: {
      type: "object",
      properties: {
        task: {
          type: "string",
          description: "Self-contained subtask. The sub-agent has none of your context — be specific.",
        },
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
  "Read src/tools.ts. In ≤3 sentences, how does the tool registry handle (a) registration and (b) dispatch? Just the answer.",
  "What is the role of the `repair` field on CacheFirstLoop? Read src/loop.ts as needed. One short paragraph.",
  "Final: in exactly 5 bullets, what should a new contributor know about how src/loop.ts + src/tools.ts interact? No preamble, no closing.",
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
  finalAssistant: string;
}

async function runMode(mode: "FLAT" | "SUB", client: DeepSeekClient): Promise<{ rows: TurnRow[]; traces: SpawnTrace[] }> {
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

  for (let i = 0; i < USER_SCRIPT.length; i++) {
    const userMsg = USER_SCRIPT[i]!;
    const turnsBefore = loop.stats.turns.length;
    let finalText = "";
    try {
      for await (const ev of loop.step(userMsg)) {
        if (ev.role === "assistant_final") finalText = ev.content;
        else if (ev.role === "done") finalText = ev.content || finalText;
        else if (ev.role === "error") throw new Error(ev.error ?? "loop error");
      }
    } catch (err) {
      finalText = `[loop error: ${(err as Error).message}]`;
    }
    const newTurns = loop.stats.turns.slice(turnsBefore);
    const promptTokens = newTurns.reduce((s, t) => s + t.usage.promptTokens, 0);
    const cacheHit = newTurns.reduce((s, t) => s + t.usage.promptCacheHitTokens, 0);
    const cacheMiss = newTurns.reduce((s, t) => s + t.usage.promptCacheMissTokens, 0);
    const completionTokens = newTurns.reduce((s, t) => s + t.usage.completionTokens, 0);
    const turnCost = newTurns.reduce((s, t) => s + t.cost, 0);
    cumulativeCost += turnCost;

    const tracesThisTurn = traces.slice(traceCountBefore);
    traceCountBefore = traces.length;
    const spawnsThisTurn = tracesThisTurn.length;
    const spawnCostThisTurn = tracesThisTurn.reduce((s, t) => s + t.costUsd, 0);
    const spawnSavingsThisTurn = tracesThisTurn.reduce((s, t) => s + t.savings, 0);
    // spawnSubagent makes its own client.chat() calls that don't flow
    // through loop.stats — add spawnCost so SUB-mode session cost is honest.
    if (mode === "SUB") cumulativeCost += spawnCostThisTurn;

    rows.push({
      userTurn: i + 1,
      modelCalls: newTurns.length,
      promptTokens,
      cacheHit,
      cacheMiss,
      completionTokens,
      costUsd: turnCost + (mode === "SUB" ? spawnCostThisTurn : 0),
      cumulativeCost,
      spawnsThisTurn,
      spawnCostThisTurn,
      spawnSavingsThisTurn,
      finalAssistant: finalText,
    });
    console.log(
      `  [${mode}] turn ${i + 1}: modelCalls=${newTurns.length} prompt=${promptTokens} hit=${cacheHit} miss=${cacheMiss} compl=${completionTokens} $${(turnCost + (mode === "SUB" ? spawnCostThisTurn : 0)).toFixed(6)} cum=$${cumulativeCost.toFixed(6)}${
        mode === "SUB" ? `  spawns=${spawnsThisTurn} spawn$=${spawnCostThisTurn.toFixed(6)} save=${spawnSavingsThisTurn}tok` : ""
      }`,
    );
  }
  return { rows, traces };
}

async function main(): Promise<void> {
  if (!process.env.DEEPSEEK_API_KEY) {
    console.error("DEEPSEEK_API_KEY missing — populate .env first.");
    process.exit(1);
  }
  console.log(`probe-session-e2e  model=${MODEL}  turns=${USER_SCRIPT.length}`);
  const client = new DeepSeekClient();

  console.log("\n=== Running FLAT ===");
  const flat = await runMode("FLAT", client);

  // 3-second gap so the second mode doesn't get a free cache warm-up from the
  // first — we want SUB to pay its own prefix-cache cost on its first call.
  await new Promise((r) => setTimeout(r, 3000));

  console.log("\n=== Running SUB ===");
  const sub = await runMode("SUB", client);

  console.log("\n=== Per-turn comparison ===");
  console.log("turn | FLAT prompt | SUB prompt | SUB compression | FLAT cum $ | SUB cum $ | SUB - FLAT");
  let crossover = -1;
  for (let i = 0; i < USER_SCRIPT.length; i++) {
    const f = flat.rows[i]!;
    const s = sub.rows[i]!;
    const compression = f.promptTokens > 0 ? s.promptTokens / f.promptTokens : 1;
    const delta = s.cumulativeCost - f.cumulativeCost;
    if (crossover < 0 && i > 0 && s.cumulativeCost <= f.cumulativeCost) crossover = i + 1;
    console.log(
      `  ${(i + 1).toString().padStart(2)} | ${f.promptTokens.toString().padStart(11)} | ${s.promptTokens.toString().padStart(10)} |    ${(compression * 100).toFixed(1)}%   | $${f.cumulativeCost.toFixed(6)} | $${s.cumulativeCost.toFixed(6)} | ${delta >= 0 ? "+" : ""}${delta.toFixed(6)}`,
    );
  }
  console.log(`\nCrossover (SUB cumulative ≤ FLAT cumulative): turn ${crossover > 0 ? crossover : "never within session"}`);

  const flatTotal = flat.rows[flat.rows.length - 1]!.cumulativeCost;
  const subTotal = sub.rows[sub.rows.length - 1]!.cumulativeCost;
  const subSpawns = sub.traces.length;
  const subSpawnTotalSavings = sub.traces.reduce((s, t) => s + t.savings, 0);
  const subSpawnTotalCost = sub.traces.reduce((s, t) => s + t.costUsd, 0);
  console.log("\n=== Totals ===");
  console.log(`  FLAT session cost:  $${flatTotal.toFixed(6)}`);
  console.log(`  SUB session cost:   $${subTotal.toFixed(6)}  (parent + ${subSpawns} spawns)`);
  console.log(`  delta:              ${subTotal >= flatTotal ? "+" : ""}${(subTotal - flatTotal).toFixed(6)}  (${(((subTotal - flatTotal) / flatTotal) * 100).toFixed(1)}% vs FLAT)`);
  console.log(`  SUB spawns total:   ${subSpawns}  cost=$${subSpawnTotalCost.toFixed(6)}  savings=${subSpawnTotalSavings}tok`);

  // Cumulative spawn savings × hit-token price tells us how much future
  // parent turns AFTER this session would save. Project forward.
  const hitTokenPrice = 0.027 / 1_000_000;
  const futurePerTurnSavings = subSpawnTotalSavings * hitTokenPrice;
  console.log(`  projected per-future-turn $ saving:  $${futurePerTurnSavings.toFixed(8)} (savings × $0.027/M hit input)`);
  if (futurePerTurnSavings > 0) {
    const breakEvenFutureTurns = Math.max(0, subTotal - flatTotal) / futurePerTurnSavings;
    console.log(`  break-even future turns: ${breakEvenFutureTurns.toFixed(1)} (after the ${USER_SCRIPT.length}-turn session ended)`);
  }

  console.log("\nJSON:");
  console.log(
    JSON.stringify(
      {
        model: MODEL,
        turns: USER_SCRIPT.length,
        flat: { rows: flat.rows.map(stripText), total: flatTotal },
        sub: {
          rows: sub.rows.map(stripText),
          total: subTotal,
          traces: sub.traces,
          spawnTotalCost: subSpawnTotalCost,
          spawnTotalSavings: subSpawnTotalSavings,
        },
        crossover,
      },
      null,
      2,
    ),
  );
}

function stripText(r: TurnRow) {
  const { finalAssistant: _drop, ...rest } = r;
  return rest;
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
