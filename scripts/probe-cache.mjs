/**
 * Probes whether mutating a mid-history message destroys DeepSeek's prompt
 * cache for everything after the mutation point.
 *
 * Hypothesis: our compactInPlace() rewrites old tool results, which shifts
 * the byte offsets of every subsequent message. DeepSeek caches by exact
 * prefix, so the next request would cache-hit only up to the mutation
 * point, even though most of the conversation is unchanged.
 *
 * Run: node scripts/probe-cache.mjs
 * Reads DEEPSEEK_API_KEY / DEEPSEEK_BASE_URL from .env.testbak.
 */

import { readFileSync } from "node:fs";

function loadDotenv(path) {
  const txt = readFileSync(path, "utf8");
  for (const line of txt.split(/\r?\n/)) {
    const m = line.match(/^([A-Z_][A-Z0-9_]*)=(.*)$/);
    if (m) process.env[m[1]] ??= m[2];
  }
}
loadDotenv("./.env.testbak");

const KEY = process.env.DEEPSEEK_API_KEY;
const BASE = process.env.DEEPSEEK_BASE_URL ?? "https://api.deepseek.com";
if (!KEY) throw new Error("DEEPSEEK_API_KEY missing");

const filler = (label, n) =>
  Array.from({ length: n }, (_, i) => `${label} line ${i}: lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.`).join("\n");

const SYS = "You are a terse echo bot. Reply with a single short sentence.";
const MSG_A_FULL = `Long context block A. Detail follows:\n${filler("A", 80)}`;
const MSG_A_TRUNCATED = `Long context block A. Detail follows:\n${filler("A", 8)}\n[truncated]`;
const MSG_B = `Block B reference: ${filler("B", 30)}`;
const MSG_C = `Block C reference: ${filler("C", 30)}`;

async function call(label, messages) {
  const t0 = Date.now();
  const res = await fetch(`${BASE}/chat/completions`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      authorization: `Bearer ${KEY}`,
    },
    body: JSON.stringify({
      model: "deepseek-chat",
      messages,
      temperature: 0,
      max_tokens: 16,
      stream: false,
    }),
  });
  const ms = Date.now() - t0;
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${label} http ${res.status}: ${text.slice(0, 300)}`);
  }
  const j = await res.json();
  const u = j.usage ?? {};
  const hit = u.prompt_cache_hit_tokens ?? 0;
  const miss = u.prompt_cache_miss_tokens ?? 0;
  const prompt = u.prompt_tokens ?? 0;
  const ratio = hit + miss > 0 ? (hit / (hit + miss)) * 100 : 0;
  console.log(
    `[${label}] prompt=${prompt} hit=${hit} miss=${miss} hit%=${ratio.toFixed(1)} ${ms}ms`,
  );
  return { hit, miss, prompt };
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function main() {
  console.log("=== Phase 1: warm cache with full conversation ===");
  const warm = [
    { role: "system", content: SYS },
    { role: "user", content: MSG_A_FULL },
    { role: "assistant", content: "ack A" },
    { role: "user", content: MSG_B },
    { role: "assistant", content: "ack B" },
    { role: "user", content: MSG_C },
    { role: "assistant", content: "ack C" },
    { role: "user", content: "say hi" },
  ];
  await call("warm-1", warm);
  await sleep(1500);
  await call("warm-2", warm);

  console.log("\n=== Phase 2: append a new turn (cache should hit on prefix) ===");
  const appended = [
    ...warm,
    { role: "assistant", content: "hi" },
    { role: "user", content: "say bye" },
  ];
  await sleep(1500);
  const appendResult = await call("append", appended);

  console.log("\n=== Phase 3: mutate MSG_A in place + append same new turn ===");
  const mutated = [
    { role: "system", content: SYS },
    { role: "user", content: MSG_A_TRUNCATED },
    { role: "assistant", content: "ack A" },
    { role: "user", content: MSG_B },
    { role: "assistant", content: "ack B" },
    { role: "user", content: MSG_C },
    { role: "assistant", content: "ack C" },
    { role: "user", content: "say hi" },
    { role: "assistant", content: "hi" },
    { role: "user", content: "say bye" },
  ];
  await sleep(1500);
  const mutateResult = await call("mutate", mutated);

  console.log("\n=== Result (Phases 1-3) ===");
  const lostHit = appendResult.hit - mutateResult.hit;
  console.log(
    `append: hit=${appendResult.hit}/${appendResult.prompt}  mutate: hit=${mutateResult.hit}/${mutateResult.prompt}`,
  );
  console.log(`cache tokens lost to in-place mutation: ${lostHit}`);
  if (lostHit > 100) {
    console.log("VERDICT: in-place mutation destroys cache. Hypothesis confirmed.");
  } else {
    console.log("VERDICT: cache survived mutation. Hypothesis rejected.");
  }

  console.log("\n=== Phase 4: append-only across many turns (the new code's behavior) ===");
  const session = [
    { role: "system", content: SYS },
    { role: "user", content: MSG_A_FULL },
    { role: "assistant", content: "ack A" },
    { role: "user", content: MSG_B },
    { role: "assistant", content: "ack B" },
  ];
  const ratios = [];
  for (let i = 0; i < 5; i++) {
    session.push({ role: "user", content: `turn ${i}: ${filler(`Q${i}`, 10)}` });
    await sleep(1200);
    const r = await call(`turn-${i}`, session);
    session.push({ role: "assistant", content: `reply ${i}` });
    const total = r.hit + r.miss;
    ratios.push(total > 0 ? (r.hit / total) * 100 : 0);
  }
  const avg = ratios.reduce((a, b) => a + b, 0) / ratios.length;
  console.log(`\nappend-only cache hit % across 5 turns: ${ratios.map((x) => x.toFixed(1)).join(", ")}`);
  console.log(`average: ${avg.toFixed(1)}%`);
  if (avg > 80) {
    console.log("VERDICT: append-only keeps cache warm across turns. New strategy validated.");
  } else {
    console.log("VERDICT: cache hit lower than expected — investigate.");
  }
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
