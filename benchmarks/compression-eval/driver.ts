import { readFileSync, writeFileSync } from "node:fs";
import { resolvePreset } from "../../src/cli/ui/presets.js";
import { DeepSeekClient } from "../../src/client.js";
import { codeSystemPrompt } from "../../src/code/prompt.js";
import { buildCodeToolset } from "../../src/code/setup.js";
import { loadApiKey, loadBaseUrl, loadPreset } from "../../src/config.js";
import { loadDotenv } from "../../src/env.js";
import { CacheFirstLoop } from "../../src/loop.js";
import { ImmutablePrefix } from "../../src/memory/runtime.js";
import { DEEPSEEK_CONTEXT_TOKENS } from "../../src/telemetry/stats.js";

interface DriverOptions {
  taskFile: string;
  transcriptOut: string;
  budgetUsd: number;
  rootDir: string;
  /** Override DEEPSEEK_CONTEXT_TOKENS to force fold triggers on short tasks. */
  fakeCtxMax: number | null;
}

function parseArgs(argv: string[]): DriverOptions {
  let taskFile = "";
  let transcriptOut = "";
  let budgetUsd = 2.0;
  let rootDir = process.cwd();
  let fakeCtxMax: number | null = null;
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i]!;
    if (a === "--task-file") taskFile = argv[++i]!;
    else if (a === "--transcript-out") transcriptOut = argv[++i]!;
    else if (a === "--budget") budgetUsd = Number(argv[++i]);
    else if (a === "--root") rootDir = argv[++i]!;
    else if (a === "--fake-ctxmax") fakeCtxMax = Number(argv[++i]);
  }
  if (!taskFile || !transcriptOut) {
    console.error(
      "usage: driver.ts --task-file <p> --transcript-out <p> [--budget 2] [--root <dir>] [--fake-ctxmax N]",
    );
    process.exit(1);
  }
  return { taskFile, transcriptOut, budgetUsd, rootDir, fakeCtxMax };
}

interface TurnRecord {
  turn: number;
  role: string;
  prompt_tokens?: number;
  cache_hit_tokens?: number;
  cache_miss_tokens?: number;
  cost_usd?: number;
  tool?: string;
  content_preview?: string;
}

async function main(): Promise<void> {
  loadDotenv();
  const opts = parseArgs(process.argv.slice(2));
  const key = loadApiKey();
  if (!key) {
    console.error("DEEPSEEK_API_KEY missing — run `reasonix setup` first");
    process.exit(1);
  }
  process.env.DEEPSEEK_API_KEY = key;

  const presetSettings = resolvePreset(loadPreset());
  const model = presetSettings.model;
  if (opts.fakeCtxMax !== null && opts.fakeCtxMax > 0) {
    for (const k of Object.keys(DEEPSEEK_CONTEXT_TOKENS)) {
      DEEPSEEK_CONTEXT_TOKENS[k] = opts.fakeCtxMax;
    }
    console.log(`ctxMax override (ALL deepseek models): -> ${opts.fakeCtxMax}`);
  }
  console.log(`model: ${model}`);
  console.log(`root:  ${opts.rootDir}`);
  console.log(`budget: $${opts.budgetUsd}`);

  const task = readFileSync(opts.taskFile, "utf8").trim();
  console.log(`task: ${task.length} chars`);

  const toolset = await buildCodeToolset({ rootDir: opts.rootDir });
  console.log(`tools: ${toolset.tools.specs().length} registered`);

  const client = new DeepSeekClient({ baseUrl: loadBaseUrl() });
  const prefix = new ImmutablePrefix({
    system: codeSystemPrompt(opts.rootDir),
    toolSpecs: toolset.tools.specs(),
  });
  const loop = new CacheFirstLoop({
    client,
    prefix,
    tools: toolset.tools,
    model,
    budgetUsd: opts.budgetUsd,
  });

  const records: TurnRecord[] = [];
  const t0 = Date.now();
  let lastTurn = 0;
  try {
    for await (const ev of loop.step(task)) {
      if (ev.role === "assistant_final" && ev.stats?.usage) {
        const u = ev.stats.usage;
        const rec: TurnRecord = {
          turn: ev.turn,
          role: "assistant_final",
          prompt_tokens: u.promptTokens,
          cache_hit_tokens: u.promptCacheHitTokens,
          cache_miss_tokens: u.promptCacheMissTokens,
          cost_usd: ev.stats.cost,
          content_preview: (ev.content ?? "").slice(0, 200),
        };
        records.push(rec);
        lastTurn = ev.turn;
        const ratio = u.promptCacheHitTokens / (u.promptTokens || 1);
        process.stdout.write(
          `  turn ${ev.turn}: pt=${u.promptTokens} hit=${(ratio * 100).toFixed(1)}% cost=$${ev.stats.cost.toFixed(4)}\n`,
        );
      } else if (ev.role === "tool" && ev.toolName) {
        records.push({
          turn: ev.turn,
          role: "tool",
          tool: ev.toolName,
          content_preview: (ev.content ?? "").slice(0, 200),
        });
      } else if (ev.role === "warning" || ev.role === "status") {
        const lc = (ev.content ?? "").toLowerCase();
        const isFold = lc.includes("fold") || lc.includes("compact") || lc.includes("summar");
        records.push({
          turn: ev.turn,
          role: isFold ? "fold_event" : `${ev.role}_event`,
          content_preview: ev.content,
        });
        if (isFold) {
          process.stdout.write(
            `  ⟳ ${ev.role.toUpperCase()} at turn ${ev.turn}: ${ev.content.slice(0, 120)}\n`,
          );
        }
      } else if (ev.role === "error") {
        records.push({ turn: ev.turn, role: "error", content_preview: ev.error });
        process.stdout.write(`  ✗ error turn ${ev.turn}: ${ev.error}\n`);
      } else if (ev.role === "done") {
        process.stdout.write(`  ✓ done (turn ${ev.turn})\n`);
      }
    }
  } catch (err) {
    process.stdout.write(`  ✗ loop threw: ${(err as Error).message}\n`);
  }

  const elapsed = Math.round((Date.now() - t0) / 1000);
  const s = loop.stats.summary();
  console.log(
    `\nDONE turns=${lastTurn} elapsed=${elapsed}s cache=${(s.cacheHitRatio * 100).toFixed(1)}% cost=$${s.totalCostUsd.toFixed(4)}`,
  );

  writeFileSync(
    opts.transcriptOut,
    `${JSON.stringify({ _meta: { model, task_length: task.length, elapsed_s: elapsed, summary: s } })}\n${records.map((r) => JSON.stringify(r)).join("\n")}\n`,
  );
  console.log(`transcript: ${opts.transcriptOut}`);
}

main().catch((err) => {
  console.error("driver crashed:", err);
  process.exit(1);
});
