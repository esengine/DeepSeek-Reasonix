/**
 * End-to-end cache probe — drives CacheFirstLoop through real turns
 * against the live DeepSeek API and reports cache hit % per turn.
 *
 * The point: validate that the post-PR code (no auto-compaction)
 * actually sustains high cache hit on a long-ish session, not just
 * that the API-level append-vs-mutate primitive behaves as expected.
 *
 * Run: REASONIX_LOG_LEVEL=ERROR npx tsx scripts/probe-loop-cache.mts
 * Reads DEEPSEEK_API_KEY from the environment, .env.testbak, or ~/.reasonix/config.json.
 */

import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { CacheFirstLoop } from "../src/loop.js";
import { DeepSeekClient } from "../src/client.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
import type { CacheChurnReason, TurnStats } from "../src/telemetry/stats.js";
import { ToolRegistry } from "../src/tools.js";

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

const filler = (label: string, n: number): string =>
  Array.from(
    { length: n },
    (_, i) =>
      `${label} line ${i}: lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.`,
  ).join("\n");

async function main() {
  const reg = new ToolRegistry();
  reg.register({
    name: "echo",
    description: "echo the input back",
    parameters: {
      type: "object",
      properties: { msg: { type: "string" } },
      required: ["msg"],
    },
    fn: async (args: Record<string, unknown>) => `echoed: ${String(args.msg ?? "")}`,
  });

  const client = new DeepSeekClient();
  const loop = new CacheFirstLoop({
    client,
    prefix: new ImmutablePrefix({
      system:
        "You are a terse echo bot. Reply with one short sentence. Do not call any tools unless explicitly asked.",
      toolSpecs: reg.specs(),
    }),
    tools: reg,
    stream: false,
    maxToolIters: 4,
  });

  // Pre-seed log with a moderate prior conversation (~6k tokens of
  // user/assistant turns) so the cache has something substantial to
  // hit across subsequent turns.
  loop.log.append({ role: "user", content: `prior context: ${filler("ctx", 60)}` });
  loop.log.append({ role: "assistant", content: "noted." });
  loop.log.append({ role: "user", content: `more context: ${filler("more", 40)}` });
  loop.log.append({ role: "assistant", content: "noted." });

  const ratios: number[] = [];
  const observedChurn: Array<{ turn: number; reasons: CacheChurnReason[] }> = [];
  let rawRewrites = 0;
  const origCompactInPlace = loop.log.compactInPlace.bind(loop.log);
  loop.log.compactInPlace = (...args: Parameters<typeof origCompactInPlace>) => {
    rawRewrites++;
    return origCompactInPlace(...args);
  };

  for (let i = 0; i < 6; i++) {
    let stats: TurnStats | null = null;
    const rewritesBefore = rawRewrites;
    for await (const ev of loop.step(`Turn ${i}: just say "ok ${i}".`)) {
      if (ev.role === "assistant_final" && ev.stats?.usage) {
        stats = ev.stats;
      }
    }
    const usage = stats?.usage;
    if (!usage) {
      console.log(`turn-${i}: no usage captured`);
      ratios.push(0);
      continue;
    }
    const ratio = cacheRatio(usage);
    const reasons = stats.cacheDiagnostics?.prefixChangeReasons ?? [];
    observedChurn.push({ turn: i, reasons: [...reasons] });
    console.log(
      `turn-${i}: prompt=${usage.promptTokens} hit=${usage.promptCacheHitTokens} miss=${usage.promptCacheMissTokens} hit%=${ratio.toFixed(1)} rawRewrites=${rawRewrites - rewritesBefore} churn=${formatReasons(reasons)}`,
    );
    ratios.push(ratio);
  }

  console.log(`\ntotal raw log.compactInPlace() calls: ${rawRewrites}`);
  console.log(`cache hit % per turn: ${ratios.map((x) => x.toFixed(1)).join(", ")}`);

  const warmRatios = ratios.slice(1);
  const avgWarm = warmRatios.reduce((a, b) => a + b, 0) / warmRatios.length;
  console.log(`warm-turn average (excluding cold start): ${avgWarm.toFixed(1)}%`);

  const unstablePrefixTurns = observedChurn
    .slice(1)
    .filter((entry) => entry.reasons.some((reason) => reason !== "log_rewrite"));
  if (unstablePrefixTurns.length > 0) {
    const detail = unstablePrefixTurns
      .map((entry) => `turn-${entry.turn}:${formatReasons(entry.reasons)}`)
      .join(", ");
    console.log(`\nFAIL: non-log prefix churn detected (${detail})`);
    process.exit(1);
  }
  if (avgWarm < 80) {
    console.log(`\nFAIL: warm-turn average ${avgWarm.toFixed(1)}% below 80% threshold`);
    process.exit(1);
  }
  console.log(
    `\nPASS: cache stayed warm (avg ${avgWarm.toFixed(1)}%); raw rewrites are reported for diagnosis, not treated as cache failure`,
  );
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
