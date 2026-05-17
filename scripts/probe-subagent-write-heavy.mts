/**
 * Round 3a — write-heavy negative case for sub-agent distillation.
 *
 * Round 2 measured the strength case: read-heavy spawns compress
 * ~17× because the work is investigation and the output is a short
 * summary. This probe runs the inverse: spawns whose deliverable IS
 * the artifact. Compression should drop sharply — possibly above 1.0
 * (output longer than the rest of the child's completion) on tasks
 * where the child just emits the artifact and stops.
 *
 * If write-heavy spawns compress to ~1.0, the distillation argument
 * stops being universal: spawn for reads, inline writes. That's a
 * meaningful design constraint and the metric should make it visible.
 *
 * Run: npx tsx scripts/probe-subagent-write-heavy.mts
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
    description: "List entries in a dir under ./src. Returns one path per line.",
    parameters: {
      type: "object",
      properties: { path: { type: "string", description: "Relative to ./src." } },
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
    description: `Read a file under ./src (up to ${MAX_READ_CHARS} chars).`,
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
  brief: string;
  /** Why this one tests write-heavy: what's the artifact? */
  artifactDescription: string;
}

const TASKS: Task[] = [
  {
    id: "lru-class-pure-write",
    brief:
      "Write a complete TypeScript class `LRUCache<K, V>` with: constructor(capacity: number), get(key: K): V | undefined, set(key: K, value: V): void, has(key: K): boolean, size(): number, clear(): void. Use a Map for O(1) access + LRU ordering. Include a single trailing usage example as a comment. Output ONLY the code, no prose, no explanation.",
    artifactDescription: "full class implementation — no reading needed",
  },
  {
    id: "jsdoc-from-source",
    brief:
      "Read ./src/tokenizer.ts. Find the function `countTokens`. Write a one-line JSDoc comment (`/** ... */`) for it that follows this repo's convention: behavior-focused, no @param/@returns, under 80 chars. Output ONLY the JSDoc line, nothing else.",
    artifactDescription: "single one-line JSDoc — heavily compressed but only ~12 tokens of output",
  },
  {
    id: "refactor-emit-block",
    brief:
      "Refactor the following function to use early returns and remove the nested if-else. Output ONLY the refactored function, no prose:\n\n```ts\nfunction classify(n: number): string {\n  let result: string;\n  if (n < 0) {\n    result = 'negative';\n  } else {\n    if (n === 0) {\n      result = 'zero';\n    } else {\n      if (n < 10) {\n        result = 'small';\n      } else {\n        result = 'large';\n      }\n    }\n  }\n  return result;\n}\n```",
    artifactDescription: "refactor, output ≈ size of input",
  },
];

interface SpawnRecord {
  task: string;
  artifact: string;
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
      "You are a code-emitter. Use the tools sparingly — at most one read if the task references a file, none otherwise. Return ONLY the requested artifact (code, JSDoc, etc) with no surrounding prose, no preamble, no closing remarks. Wrap code in a single ```ts block when appropriate.",
    task: task.brief,
    model: MODEL,
    maxToolIters: 8,
  });
  const elapsedMs = Date.now() - t0;
  const outputTokens = countTokens(result.output);
  const savings = result.usage.completionTokens - outputTokens;
  const compressionRatio = result.usage.completionTokens > 0 ? outputTokens / result.usage.completionTokens : 1;
  return {
    task: task.id,
    artifact: task.artifactDescription,
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

const fmtPct = (x: number) => `${(x * 100).toFixed(1)}%`;

async function main(): Promise<void> {
  if (!process.env.DEEPSEEK_API_KEY) {
    console.error("DEEPSEEK_API_KEY missing — populate .env first.");
    process.exit(1);
  }
  console.log(`probe-subagent-write-heavy  model=${MODEL}`);
  console.log(`tasks: ${TASKS.length}`);

  const client = new DeepSeekClient();
  const records: SpawnRecord[] = [];
  for (const t of TASKS) {
    console.log(`\n--- ${t.id} ---  (${t.artifactDescription})`);
    const rec = await runOne(client, t);
    records.push(rec);
    const savingsLabel = rec.savings >= 0 ? `${rec.savings}tok saved` : `${-rec.savings}tok ADDED`;
    console.log(
      `  turns=${rec.turns}  toolIters=${rec.toolIters}  completion=${rec.completionTokens}  out=${rec.outputTokens}tok/${rec.outputChars}ch  ${savingsLabel}  compression=${fmtPct(rec.compressionRatio)}  $${rec.costUsd.toFixed(6)}  ${(rec.elapsedMs / 1000).toFixed(1)}s`,
    );
    console.log(`  output (first 220 chars): ${rec.output.replace(/\s+/g, " ").slice(0, 220)}${rec.output.length > 220 ? "…" : ""}`);
  }

  const totals = records.reduce(
    (a, r) => {
      a.promptTokens += r.promptTokens;
      a.completionTokens += r.completionTokens;
      a.outputTokens += r.outputTokens;
      a.savings += r.savings;
      a.costUsd += r.costUsd;
      a.elapsedMs += r.elapsedMs;
      return a;
    },
    { promptTokens: 0, completionTokens: 0, outputTokens: 0, savings: 0, costUsd: 0, elapsedMs: 0 },
  );
  const aggCompression = totals.completionTokens > 0 ? totals.outputTokens / totals.completionTokens : 1;

  console.log("\n=== AGGREGATE (write-heavy) ===");
  console.log(`  spawns: ${records.length}`);
  console.log(`  child completion tok: ${totals.completionTokens}`);
  console.log(`  child output tok:     ${totals.outputTokens}`);
  console.log(`  savings (lb):         ${totals.savings} tokens  ${totals.savings < 0 ? "← NEGATIVE: spawn cost > inline cost" : ""}`);
  console.log(`  compression:          ${fmtPct(aggCompression)}`);
  console.log(`  total cost:           $${totals.costUsd.toFixed(6)}`);

  console.log("\nJSON:");
  console.log(
    JSON.stringify(
      {
        model: MODEL,
        spawns: records.length,
        totals: { ...totals, aggCompression },
        records: records.map((r) => ({
          task: r.task,
          artifact: r.artifact,
          turns: r.turns,
          toolIters: r.toolIters,
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
