/**
 * Long-session probe — drives CacheFirstLoop through 20 real turns
 * with oversized tool results (each ~4k tokens, the size that USED to
 * trigger the old turn-end auto-compaction every turn).
 *
 * Reports per-turn: prompt size, cache hit %, miss tokens, USD cost.
 * Surfaces: cache trajectory, cost shape, anything degrading over time.
 *
 * Run: REASONIX_LOG_LEVEL=ERROR npx tsx scripts/probe-long-session.mts
 */

import { readFileSync } from "node:fs";
import { DeepSeekClient } from "../src/client.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
import { DEEPSEEK_CONTEXT_TOKENS } from "../src/telemetry/stats.js";
import { ToolRegistry } from "../src/tools.js";

// Force a small ctx window so the 50% fold threshold trips in a few
// turns instead of needing 200+ turns at the real 1M cap. Same model
// id, real API call, just the local gauge is shrunk.
DEEPSEEK_CONTEXT_TOKENS["deepseek-chat"] = 50_000;

function loadDotenv(path: string) {
  const txt = readFileSync(path, "utf8");
  for (const line of txt.split(/\r?\n/)) {
    const m = line.match(/^([A-Z_][A-Z0-9_]*)=(.*)$/);
    if (m && !process.env[m[1]]) process.env[m[1]] = m[2];
  }
}
loadDotenv("./.env.testbak");

const PRICE_HIT_PER_M = 0.07;
const PRICE_MISS_PER_M = 0.27;
const PRICE_COMPLETION_PER_M = 1.1;

const docLine = (i: number, sec: string) =>
  `[${sec}#${i}] section ${sec} entry ${i}: requirement traces to constraint ${(i % 7) + 1}, status ${i % 3 === 0 ? "open" : "closed"}, owner team-${(i % 5) + 1}, last touched 2026-04-${(i % 28) + 1}.`;

async function main() {
  const reg = new ToolRegistry();
  reg.register({
    name: "read_doc",
    description: "Read a section of a project document.",
    parameters: {
      type: "object",
      properties: { section: { type: "string" } },
      required: ["section"],
    },
    fn: async (args: Record<string, unknown>) => {
      const sec = String(args.section ?? "default");
      const lines = Array.from({ length: 65 }, (_, i) => docLine(i, sec));
      return lines.join("\n");
    },
  });

  const client = new DeepSeekClient();
  const model = "deepseek-chat";
  const loop = new CacheFirstLoop({
    client,
    prefix: new ImmutablePrefix({
      system:
        "You are a documentation triage agent. For each turn, call read_doc with the section the user asks about, then reply with one short sentence summarizing what you found.",
      toolSpecs: reg.specs(),
    }),
    tools: reg,
    stream: false,
    model,
    maxToolIters: 4,
  });

  let mutations = 0;
  let folds = 0;
  const origCompactInPlace = loop.log.compactInPlace.bind(loop.log);
  loop.log.compactInPlace = (...args) => {
    mutations++;
    return origCompactInPlace(...args);
  };

  const sections = [
    "auth",
    "billing",
    "telemetry",
    "rate-limit",
    "webhooks",
    "search",
    "indexing",
    "permissions",
    "audit",
    "exports",
    "imports",
    "cdn",
    "analytics",
    "rbac",
    "sso",
    "scheduler",
    "workflows",
    "notifications",
    "reports",
    "api-v2",
  ];

  console.log("turn |  prompt  |   hit   |   miss  | hit% |  $/turn  |  $cum");
  console.log("-----+----------+---------+---------+------+----------+--------");

  let cumCost = 0;
  let forceSummaryHit = false;

  for (let i = 0; i < sections.length; i++) {
    const t0 = Date.now();
    let usage: {
      promptTokens: number;
      completionTokens: number;
      promptCacheHitTokens: number;
      promptCacheMissTokens: number;
    } | null = null;
    let warning = "";
    for await (const ev of loop.step(`Read the "${sections[i]}" section.`)) {
      if (ev.role === "assistant_final" && ev.stats?.usage) {
        usage = ev.stats.usage as typeof usage;
      }
      if (ev.role === "warning" && ev.content) {
        warning = ev.content;
        if (/folded \d+ messages/.test(ev.content)) folds++;
      }
      if (ev.forcedSummary) forceSummaryHit = true;
    }
    const ms = Date.now() - t0;
    if (!usage) {
      console.log(
        `${String(i).padStart(3)}  | (no usage)  -- ${warning ? `warning: ${warning}` : ""}`,
      );
      continue;
    }
    const total = usage.promptCacheHitTokens + usage.promptCacheMissTokens;
    const ratio = total > 0 ? (usage.promptCacheHitTokens / total) * 100 : 0;
    const cost =
      (usage.promptCacheHitTokens * PRICE_HIT_PER_M +
        usage.promptCacheMissTokens * PRICE_MISS_PER_M +
        usage.completionTokens * PRICE_COMPLETION_PER_M) /
      1_000_000;
    cumCost += cost;
    console.log(
      `${String(i).padStart(3)}  | ${String(usage.promptTokens).padStart(7)} | ${String(usage.promptCacheHitTokens).padStart(7)} | ${String(usage.promptCacheMissTokens).padStart(7)} | ${ratio.toFixed(1).padStart(4)} | $${cost.toFixed(5)} | $${cumCost.toFixed(4)}  ${ms}ms${warning ? ` (${warning.slice(0, 60)}…)` : ""}`,
    );
    if (forceSummaryHit) {
      console.log(
        `\n>> force-summary triggered at turn ${i} (${ratio.toFixed(1)}% cache hit, ${usage.promptTokens} tokens)`,
      );
      break;
    }
  }

  console.log(`\ntotal log.compactInPlace() calls: ${mutations} (expected: ${folds} folds)`);
  console.log(`total cost across session: $${cumCost.toFixed(4)}`);

  if (mutations !== folds) {
    console.log(`FAIL: ${mutations} mutations but only ${folds} fold events — unexpected mutation`);
    process.exit(1);
  }
  console.log(`\nVERDICT: ${folds} fold(s) fired, no other mutations.`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
