/**
 * Measure distillation savings of spawn_subagent on read-heavy tasks.
 *
 * Headline number: how many tokens does the parent's append-only log
 * NOT have to absorb because the child returned a single distilled
 * string instead of running its work inline in the parent's context?
 *
 * Per spawn:
 *   compressionRatio  = outputTokens / completionTokens
 *   savingsPerSpawn   = completionTokens - outputTokens
 *   (lower bound — ignores tool result tokens, which would also have
 *    landed in the parent log inline. So real savings ≥ this number.)
 *
 * Run: npx tsx scripts/probe-subagent-distillation.mts
 * Reads DEEPSEEK_API_KEY from .env (then .env.testbak).
 */

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { DeepSeekClient } from "../src/client.js";
import { spawnSubagent } from "../src/tools/subagent.js";
import { ToolRegistry } from "../src/tools.js";
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
    description: "List entries (files + subdirs) in a directory under ./src. Returns one path per line, prefixed with 'd ' for dirs and 'f ' for files.",
    parameters: {
      type: "object",
      properties: {
        path: { type: "string", description: "Relative to repo's ./src. Use '' for src root." },
      },
      required: ["path"],
    },
    fn: async (args: { path?: string }) => {
      const rel = (args.path ?? "").replace(/^[/\\]+/, "");
      const abs = join(ROOT, rel);
      if (!abs.startsWith(ROOT)) return "error: path escapes ./src";
      if (!existsSync(abs)) return `error: not found: ${rel}`;
      const out: string[] = [];
      for (const name of readdirSync(abs).sort()) {
        const child = join(abs, name);
        const st = statSync(child);
        out.push(`${st.isDirectory() ? "d" : "f"} ${rel ? `${rel}/${name}` : name}`);
      }
      return out.join("\n") || "(empty)";
    },
  });

  reg.register({
    name: "read_file",
    description: `Read a file under ./src. Returns up to ${MAX_READ_CHARS} chars from the start.`,
    parameters: {
      type: "object",
      properties: {
        path: { type: "string", description: "Path relative to repo's ./src." },
      },
      required: ["path"],
    },
    fn: async (args: { path?: string }) => {
      const rel = (args.path ?? "").replace(/^[/\\]+/, "");
      const abs = join(ROOT, rel);
      if (!abs.startsWith(ROOT)) return "error: path escapes ./src";
      if (!existsSync(abs)) return `error: not found: ${rel}`;
      const text = readFileSync(abs, "utf8");
      if (text.length <= MAX_READ_CHARS) return text;
      return `${text.slice(0, MAX_READ_CHARS)}\n[truncated — file is ${text.length} chars]`;
    },
  });

  return reg;
}

interface Task {
  id: string;
  brief: string;
  /** What we expect the child to roughly produce — used as a sanity check, not a pass/fail gate. */
  shape: string;
}

const TASKS: Task[] = [
  {
    id: "summarize-index",
    brief:
      "Read ./src/index.ts and report the FIVE most important named exports plus a single phrase (≤8 words) describing each. Return exactly five bullets — no preamble, no closing.",
    shape: "5 bullets",
  },
  {
    id: "list-tools",
    brief:
      "List every file directly inside ./src/tools (use list_dir on 'tools'). For each .ts file (skip subdirectories), give the filename and one short sentence (≤12 words) describing what the tool does. Use the file's top docstring or first export name as your evidence. Return one bullet per file, nothing else.",
    shape: "one bullet per src/tools/*.ts",
  },
  {
    id: "loop-classes",
    brief:
      "Read ./src/loop.ts. Identify the THREE classes (top-level `export class` or `class` declarations) with the most lines of code. For each, give: ClassName — approximate line count — one-sentence purpose. Three bullets, nothing else.",
    shape: "3 bullets",
  },
];

interface SpawnRecord {
  task: string;
  shape: string;
  turns: number;
  toolIters: number;
  promptTokens: number;
  completionTokens: number;
  cacheHitTokens: number;
  cacheMissTokens: number;
  outputTokens: number;
  outputChars: number;
  savings: number;
  compressionRatio: number;
  costUsd: number;
  elapsedMs: number;
  output: string;
}

async function runOne(client: DeepSeekClient, task: Task): Promise<SpawnRecord> {
  const reg = buildRegistry();
  const t0 = Date.now();
  const result = await spawnSubagent({
    client,
    parentRegistry: reg,
    system:
      "You are a read-only investigator. Use list_dir and read_file as needed, then return the distilled answer. Be terse. Output ONLY the requested format — no preamble, no closing remarks, no offers of further help.",
    task: task.brief,
    model: MODEL,
    maxToolIters: 16,
  });
  const elapsedMs = Date.now() - t0;
  const outputTokens = countTokens(result.output);
  const savings = Math.max(0, result.usage.completionTokens - outputTokens);
  const compressionRatio = result.usage.completionTokens > 0 ? outputTokens / result.usage.completionTokens : 1;
  return {
    task: task.id,
    shape: task.shape,
    turns: result.turns,
    toolIters: result.toolIters,
    promptTokens: result.usage.promptTokens,
    completionTokens: result.usage.completionTokens,
    cacheHitTokens: result.usage.promptCacheHitTokens,
    cacheMissTokens: result.usage.promptCacheMissTokens,
    outputTokens,
    outputChars: result.output.length,
    savings,
    compressionRatio,
    costUsd: result.costUsd,
    elapsedMs,
    output: result.output,
  };
}

function fmtPct(x: number): string {
  return `${(x * 100).toFixed(1)}%`;
}

async function main(): Promise<void> {
  if (!process.env.DEEPSEEK_API_KEY) {
    console.error("DEEPSEEK_API_KEY missing — populate .env first.");
    process.exit(1);
  }
  console.log(`probe-subagent-distillation  model=${MODEL}`);
  console.log(`tasks: ${TASKS.length}`);

  const client = new DeepSeekClient();
  const records: SpawnRecord[] = [];
  for (const t of TASKS) {
    console.log(`\n--- ${t.id} ---`);
    const rec = await runOne(client, t);
    records.push(rec);
    console.log(
      `  turns=${rec.turns}  toolIters=${rec.toolIters}  prompt=${rec.promptTokens}  completion=${rec.completionTokens}  hit=${rec.cacheHitTokens}  miss=${rec.cacheMissTokens}  out=${rec.outputTokens}tok/${rec.outputChars}ch  savings=${rec.savings}tok  compression=${fmtPct(rec.compressionRatio)}  $${rec.costUsd.toFixed(6)}  ${(rec.elapsedMs / 1000).toFixed(1)}s`,
    );
    console.log(`  output: ${rec.output.replace(/\s+/g, " ").slice(0, 240)}${rec.output.length > 240 ? "…" : ""}`);
  }

  const totals = records.reduce(
    (acc, r) => {
      acc.promptTokens += r.promptTokens;
      acc.completionTokens += r.completionTokens;
      acc.cacheHitTokens += r.cacheHitTokens;
      acc.cacheMissTokens += r.cacheMissTokens;
      acc.outputTokens += r.outputTokens;
      acc.savings += r.savings;
      acc.costUsd += r.costUsd;
      acc.elapsedMs += r.elapsedMs;
      return acc;
    },
    {
      promptTokens: 0,
      completionTokens: 0,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      outputTokens: 0,
      savings: 0,
      costUsd: 0,
      elapsedMs: 0,
    },
  );

  const aggCompression = totals.completionTokens > 0 ? totals.outputTokens / totals.completionTokens : 1;
  const aggHitRate =
    totals.cacheHitTokens + totals.cacheMissTokens > 0
      ? totals.cacheHitTokens / (totals.cacheHitTokens + totals.cacheMissTokens)
      : 0;

  console.log("\n=== AGGREGATE ===");
  console.log(`  spawns:           ${records.length}`);
  console.log(`  child prompt tok: ${totals.promptTokens}`);
  console.log(`  child completion: ${totals.completionTokens}`);
  console.log(`  child cache hit%: ${fmtPct(aggHitRate)}  (${totals.cacheHitTokens} hit / ${totals.cacheMissTokens} miss)`);
  console.log(`  child output tok: ${totals.outputTokens}   ← what the parent log grows by`);
  console.log(`  savings (lb):     ${totals.savings} tokens   ← parent log growth avoided`);
  console.log(`  compression:      ${fmtPct(aggCompression)}  ← output / completion across all spawns`);
  console.log(`  total cost:       $${totals.costUsd.toFixed(6)}`);
  console.log(`  wall clock:       ${(totals.elapsedMs / 1000).toFixed(1)}s`);

  console.log("\nJSON:");
  console.log(
    JSON.stringify(
      {
        model: MODEL,
        spawns: records.length,
        totals: { ...totals, aggCompression, aggHitRate },
        records: records.map((r) => ({
          task: r.task,
          turns: r.turns,
          toolIters: r.toolIters,
          promptTokens: r.promptTokens,
          completionTokens: r.completionTokens,
          outputTokens: r.outputTokens,
          savings: r.savings,
          compressionRatio: r.compressionRatio,
          costUsd: r.costUsd,
          elapsedMs: r.elapsedMs,
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
