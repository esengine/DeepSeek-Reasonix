/**
 * End-to-end cache probe — drives CacheFirstLoop through real turns
 * against the live DeepSeek API and reports cache hit % per turn.
 *
 * The point: validate that the post-PR code (no auto-compaction)
 * actually sustains high cache hit on a long-ish session, not just
 * that the API-level append-vs-mutate primitive behaves as expected.
 *
 * Run: REASONIX_LOG_LEVEL=ERROR npx tsx scripts/probe-loop-cache.mts
 * Reads DEEPSEEK_API_KEY from .env.testbak.
 */

import { readFileSync } from "node:fs";
import { CacheFirstLoop } from "../src/loop.js";
import { DeepSeekClient } from "../src/client.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
import { ToolRegistry } from "../src/tools.js";

function loadDotenv(path: string) {
  const txt = readFileSync(path, "utf8");
  for (const line of txt.split(/\r?\n/)) {
    const m = line.match(/^([A-Z_][A-Z0-9_]*)=(.*)$/);
    if (m && !process.env[m[1]]) process.env[m[1]] = m[2];
  }
}
loadDotenv("./.env.testbak");

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
  let mutations = 0;
  const origCompactInPlace = loop.log.compactInPlace.bind(loop.log);
  loop.log.compactInPlace = (...args: Parameters<typeof origCompactInPlace>) => {
    mutations++;
    return origCompactInPlace(...args);
  };

  for (let i = 0; i < 6; i++) {
    let usage: { promptTokens: number; promptCacheHitTokens: number; promptCacheMissTokens: number } | null = null;
    for await (const ev of loop.step(`Turn ${i}: just say "ok ${i}".`)) {
      if (ev.role === "assistant_final" && ev.stats?.usage) {
        usage = ev.stats.usage as typeof usage;
      }
    }
    if (!usage) {
      console.log(`turn-${i}: no usage captured`);
      ratios.push(0);
      continue;
    }
    const total = usage.promptCacheHitTokens + usage.promptCacheMissTokens;
    const ratio = total > 0 ? (usage.promptCacheHitTokens / total) * 100 : 0;
    console.log(
      `turn-${i}: prompt=${usage.promptTokens} hit=${usage.promptCacheHitTokens} miss=${usage.promptCacheMissTokens} hit%=${ratio.toFixed(1)}`,
    );
    ratios.push(ratio);
  }

  console.log(`\ntotal log.compactInPlace() calls: ${mutations}`);
  console.log(`cache hit % per turn: ${ratios.map((x) => x.toFixed(1)).join(", ")}`);

  const warmRatios = ratios.slice(1);
  const avgWarm = warmRatios.reduce((a, b) => a + b, 0) / warmRatios.length;
  console.log(`warm-turn average (excluding cold start): ${avgWarm.toFixed(1)}%`);

  if (mutations > 0) {
    console.log(`\nFAIL: log was mutated ${mutations} time(s) — append-only invariant broken`);
    process.exit(1);
  }
  if (avgWarm < 80) {
    console.log(`\nFAIL: warm-turn average ${avgWarm.toFixed(1)}% below 80% threshold`);
    process.exit(1);
  }
  console.log(`\nPASS: append-only sustained, cache hit avg ${avgWarm.toFixed(1)}%`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
