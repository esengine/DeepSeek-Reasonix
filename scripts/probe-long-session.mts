/**
 * Long-session probe — drives CacheFirstLoop through 20 real turns
 * with oversized tool results (each ~4k tokens, the size that USED to
 * trigger the old turn-end auto-compaction every turn).
 *
 * Reports per-turn: prompt size, cache hit %, miss tokens, USD cost.
 * Surfaces: cache trajectory, cost shape, anything degrading over time.
 *
 * Run: REASONIX_LOG_LEVEL=ERROR npx tsx scripts/probe-long-session.mts
 * Reads DEEPSEEK_API_KEY from the environment, .env.testbak, or ~/.reasonix/config.json.
 */

import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { DeepSeekClient } from "../src/client.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
import {
  DEEPSEEK_CONTEXT_TOKENS,
  pricingFor,
  type CacheChurnReason,
  type TurnStats,
} from "../src/telemetry/stats.js";
import { ToolRegistry } from "../src/tools.js";

// Force a small ctx window so the 50% fold threshold trips in a few
// turns instead of needing 200+ turns at the real 1M cap. Same model
// id, real API call, just the local gauge is shrunk.
DEEPSEEK_CONTEXT_TOKENS["deepseek-chat"] = 50_000;

function loadDotenv(path: string): void {
  if (!existsSync(path)) return;
  const txt = readFileSync(path, "utf8");
  for (const line of txt.split(/\r?\n/)) {
    const m = line.match(/^([A-Z_][A-Z0-9_]*)=(.*)$/);
    if (m && !process.env[m[1]]) process.env[m[1]] = m[2];
  }
}

function loadReasonixApiKey(): void {
  if (process.env.DEEPSEEK_API_KEY) return;
  const path = join(homedir(), ".reasonix", "config.json");
  if (!existsSync(path)) return;
  const cfg = JSON.parse(readFileSync(path, "utf8")) as {
    apiKey?: string;
    deepseekApiKey?: string;
    endpoint?: { apiKey?: string };
  };
  const key = cfg.apiKey ?? cfg.deepseekApiKey ?? cfg.endpoint?.apiKey ?? "";
  if (key) process.env.DEEPSEEK_API_KEY = key;
}

loadDotenv("./.env.testbak");
loadReasonixApiKey();

function cacheRatio(usage: {
  promptCacheHitTokens: number;
  promptCacheMissTokens: number;
}): number {
  const total = usage.promptCacheHitTokens + usage.promptCacheMissTokens;
  return total > 0 ? (usage.promptCacheHitTokens / total) * 100 : 0;
}

function formatReasons(reasons: readonly CacheChurnReason[]): string {
  return reasons.length > 0 ? reasons.join(",") : "-";
}

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

  let rawRewrites = 0;
  let folds = 0;
  const origCompactInPlace = loop.log.compactInPlace.bind(loop.log);
  loop.log.compactInPlace = (...args) => {
    rawRewrites++;
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

  console.log("turn |  prompt  |   hit   |   miss  | hit% |  $/turn  |  $cum  | churn");
  console.log("-----+----------+---------+---------+------+----------+--------+----------");

  let cumCost = 0;
  let forceSummaryHit = false;
  const measured: Array<{
    turn: number;
    ratio: number;
    forcedSummary: boolean;
    reasons: CacheChurnReason[];
  }> = [];
  const pricing = pricingFor(model);

  for (let i = 0; i < sections.length; i++) {
    const t0 = Date.now();
    let stats: TurnStats | null = null;
    let warning = "";
    let turnForcedSummary = false;
    for await (const ev of loop.step(`Read the "${sections[i]}" section.`)) {
      if (ev.role === "assistant_final" && ev.stats?.usage) {
        stats = ev.stats;
      }
      if (ev.role === "warning" && ev.content) {
        warning = ev.content;
        if (/folded \d+ messages/.test(ev.content)) folds++;
      }
      if (ev.forcedSummary) {
        forceSummaryHit = true;
        turnForcedSummary = true;
      }
    }
    const ms = Date.now() - t0;
    const usage = stats?.usage;
    if (!usage) {
      console.log(
        `${String(i).padStart(3)}  | (no usage)  -- ${warning ? `warning: ${warning}` : ""}`,
      );
      continue;
    }
    const ratio = cacheRatio(usage);
    const cost = pricing
      ? (usage.promptCacheHitTokens * pricing.inputCacheHit +
          usage.promptCacheMissTokens * pricing.inputCacheMiss +
          usage.completionTokens * pricing.output) /
        1_000_000
      : 0;
    const reasons = stats.cacheDiagnostics?.prefixChangeReasons ?? [];
    measured.push({ turn: i, ratio, forcedSummary: turnForcedSummary, reasons: [...reasons] });
    cumCost += cost;
    console.log(
      `${String(i).padStart(3)}  | ${String(usage.promptTokens).padStart(7)} | ${String(usage.promptCacheHitTokens).padStart(7)} | ${String(usage.promptCacheMissTokens).padStart(7)} | ${ratio.toFixed(1).padStart(4)} | $${cost.toFixed(5)} | $${cumCost.toFixed(4)} | ${formatReasons(reasons)}  ${ms}ms${warning ? ` (${warning.slice(0, 60)}…)` : ""}`,
    );
    if (forceSummaryHit) {
      console.log(
        `\n>> force-summary triggered at turn ${i} (${ratio.toFixed(1)}% cache hit, ${usage.promptTokens} tokens)`,
      );
      break;
    }
  }

  console.log(`\ntotal raw log.compactInPlace() calls: ${rawRewrites} (${folds} fold warning events)`);
  console.log(`total cost across session: $${cumCost.toFixed(4)}`);

  const sustained = measured.filter((entry) => entry.turn >= 4 && !entry.forcedSummary);
  const sustainedAvg =
    sustained.length > 0
      ? sustained.reduce((sum, entry) => sum + entry.ratio, 0) / sustained.length
      : 0;
  const unstablePrefixTurns = measured
    .slice(1)
    .filter((entry) => entry.reasons.some((reason) => reason !== "log_rewrite"));
  console.log(`sustained cache hit average (turn >=4, pre-summary): ${sustainedAvg.toFixed(1)}%`);

  if (unstablePrefixTurns.length > 0) {
    const detail = unstablePrefixTurns
      .map((entry) => `turn-${entry.turn}:${formatReasons(entry.reasons)}`)
      .join(", ");
    console.log(`FAIL: non-log prefix churn detected (${detail})`);
    process.exit(1);
  }
  if (sustainedAvg < 85) {
    console.log(`FAIL: sustained cache hit average ${sustainedAvg.toFixed(1)}% below 85% threshold`);
    process.exit(1);
  }
  console.log(
    `\nVERDICT: cache stayed warm through oversized tool results; raw rewrites are diagnostic, and force-summary cold miss is expected when triggered.`,
  );
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
