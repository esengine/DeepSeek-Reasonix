/**
 * End-to-end cache probe for the orchestrator + sub-context topology (RFC 0001).
 *
 * Compares two ways of doing N module-sized work units against the live
 * DeepSeek API:
 *
 *   FLAT       one growing messages[] across N turns (today's loop)
 *   ORCH       N independent messages[] sharing a frozen role prefix
 *
 * Headline measurements per mode:
 *   - total billed-equivalent input tokens (miss_tokens + hit_tokens * 0.1)
 *   - cache hit ratio across all calls
 *   - latency
 *
 * No tools, no real codegen — just exercises the prefix-cache surface.
 * Bench tasks live elsewhere; this is purely the economic question.
 *
 * Run: node scripts/probe-orchestrator-cache.mjs
 * Reads DEEPSEEK_API_KEY / DEEPSEEK_BASE_URL from .env (then .env.testbak).
 */

import { existsSync, readFileSync } from "node:fs";

function loadDotenv(path) {
  if (!existsSync(path)) return false;
  const txt = readFileSync(path, "utf8");
  for (const line of txt.split(/\r?\n/)) {
    const m = line.match(/^([A-Z_][A-Z0-9_]*)=(.*)$/);
    if (m) process.env[m[1]] ??= m[2];
  }
  return true;
}
loadDotenv("./.env") || loadDotenv("./.env.testbak");

const KEY = process.env.DEEPSEEK_API_KEY;
const BASE = process.env.DEEPSEEK_BASE_URL ?? "https://api.deepseek.com";
const MODEL = process.env.PROBE_MODEL ?? "deepseek-chat";
if (!KEY) {
  console.error("DEEPSEEK_API_KEY missing — populate .env first.");
  process.exit(1);
}

const filler = (label, n) =>
  Array.from(
    { length: n },
    (_, i) =>
      `${label} ${i}: lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua nostrud exercitation ullamco laboris nisi ut aliquip.`,
  ).join("\n");

// ~3000-token "frozen role prefix" — large enough that caching matters,
// stable enough that byte-identical reuse across spawns is realistic.
const ROLE_SYSTEM = [
  "You are coder-A, a specialist sub-agent. Your job: emit a short, idiomatic TypeScript stub for the module described in the brief.",
  "",
  "Output contract:",
  "- One ```ts code block, nothing else.",
  "- ≤ 30 lines.",
  "- No prose, no explanation, no comments outside the code block.",
  "- The module must export a default class with the methods listed in the brief.",
  "",
  "Style:",
  "- Modern TypeScript, strict-friendly.",
  "- No `any`. No `as` casts.",
  "- Prefer terse names; the brief is the spec.",
  "",
  "Anchor context (kept verbatim across every spawn to seat the prefix cache):",
  filler("anchor", 80),
].join("\n");

const MODULES = [
  { name: "Counter",   methods: "increment(): void; getCount(): number" },
  { name: "Stack",     methods: "push(x: number): void; pop(): number | undefined; peek(): number | undefined" },
  { name: "Queue",     methods: "enqueue(x: number): void; dequeue(): number | undefined" },
  { name: "RingBuffer",methods: "write(x: number): void; read(): number | undefined; isFull(): boolean" },
  { name: "Toggle",    methods: "flip(): void; isOn(): boolean" },
];

function briefFor(mod) {
  return `Implement module ${mod.name}. Methods: ${mod.methods}. Initial state empty/off/zero as appropriate. No constructor args.`;
}

async function call(label, messages) {
  const t0 = Date.now();
  const res = await fetch(`${BASE}/chat/completions`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      authorization: `Bearer ${KEY}`,
    },
    body: JSON.stringify({
      model: MODEL,
      messages,
      temperature: 0,
      max_tokens: 220,
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
  const completion = u.completion_tokens ?? 0;
  const ratio = hit + miss > 0 ? (hit / (hit + miss)) * 100 : 0;
  console.log(
    `  [${label}] prompt=${prompt} hit=${hit} miss=${miss} hit%=${ratio.toFixed(1)} completion=${completion} ${ms}ms`,
  );
  return { hit, miss, prompt, completion, ms, content: j.choices?.[0]?.message?.content ?? "" };
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function summarize(label, results) {
  const hit = results.reduce((s, r) => s + r.hit, 0);
  const miss = results.reduce((s, r) => s + r.miss, 0);
  const prompt = results.reduce((s, r) => s + r.prompt, 0);
  const completion = results.reduce((s, r) => s + r.completion, 0);
  const ms = results.reduce((s, r) => s + r.ms, 0);
  const hitPct = hit + miss > 0 ? (hit / (hit + miss)) * 100 : 0;
  // Billed-equivalent input tokens at DeepSeek's ~10% cache rate.
  const billedEq = miss + hit * 0.1;
  console.log(
    `\n  ${label} totals: prompt=${prompt} hit=${hit} miss=${miss} hit%=${hitPct.toFixed(1)} completion=${completion} billed-eq-in=${billedEq.toFixed(0)} elapsed=${(ms / 1000).toFixed(1)}s`,
  );
  return { hit, miss, prompt, completion, ms, hitPct, billedEq };
}

async function runFlat() {
  console.log("\n=== FLAT — one growing messages[] across all modules ===");
  // Same role anchor as orchestrator, sitting in the system slot so the
  // comparison is apples to apples on prefix size.
  const messages = [{ role: "system", content: ROLE_SYSTEM }];
  const results = [];
  for (let i = 0; i < MODULES.length; i++) {
    messages.push({ role: "user", content: briefFor(MODULES[i]) });
    await sleep(800);
    const r = await call(`flat-${i + 1}`, messages);
    messages.push({ role: "assistant", content: r.content });
    results.push(r);
  }
  return summarize("FLAT", results);
}

async function runOrchestrator() {
  console.log("\n=== ORCH — fresh messages[] per module, frozen role prefix ===");
  const results = [];
  for (let i = 0; i < MODULES.length; i++) {
    const messages = [
      { role: "system", content: ROLE_SYSTEM },
      { role: "user", content: briefFor(MODULES[i]) },
    ];
    await sleep(800);
    const r = await call(`orch-${i + 1}`, messages);
    results.push(r);
  }
  return summarize("ORCH", results);
}

async function main() {
  console.log(`probe-orchestrator-cache  model=${MODEL}  base=${BASE}`);
  console.log(`role_system tokens (rough char/4 estimate): ${(ROLE_SYSTEM.length / 4).toFixed(0)}`);
  console.log(`modules: ${MODULES.length}`);

  // Cold cache for both modes — run ORCH first because FLAT will warm
  // the system prefix and we'd otherwise hand ORCH a tailwind.
  // Actually: both modes share the same ROLE_SYSTEM, so whichever runs
  // first absorbs the cold-start cost. Run each twice (cold + warm)
  // and report both for honesty.

  console.log("\n--- ROUND 1 (cold start for whichever runs first) ---");
  const orchCold = await runOrchestrator();
  await sleep(1500);
  const flatCold = await runFlat();

  console.log("\n--- ROUND 2 (system prefix already warmed) ---");
  await sleep(2500);
  const orchWarm = await runOrchestrator();
  await sleep(1500);
  const flatWarm = await runFlat();

  console.log("\n=== COMPARISON ===");
  const fmt = (n) => n.toFixed(0).padStart(8);
  const fmtPct = (n) => `${n.toFixed(1)}%`.padStart(7);
  console.log("                    prompt       hit      miss   hit%    billed-eq-in");
  console.log(`  FLAT  (cold)  ${fmt(flatCold.prompt)} ${fmt(flatCold.hit)} ${fmt(flatCold.miss)}  ${fmtPct(flatCold.hitPct)}    ${fmt(flatCold.billedEq)}`);
  console.log(`  ORCH  (cold)  ${fmt(orchCold.prompt)} ${fmt(orchCold.hit)} ${fmt(orchCold.miss)}  ${fmtPct(orchCold.hitPct)}    ${fmt(orchCold.billedEq)}`);
  console.log(`  FLAT  (warm)  ${fmt(flatWarm.prompt)} ${fmt(flatWarm.hit)} ${fmt(flatWarm.miss)}  ${fmtPct(flatWarm.hitPct)}    ${fmt(flatWarm.billedEq)}`);
  console.log(`  ORCH  (warm)  ${fmt(orchWarm.prompt)} ${fmt(orchWarm.hit)} ${fmt(orchWarm.miss)}  ${fmtPct(orchWarm.hitPct)}    ${fmt(orchWarm.billedEq)}`);

  const coldDelta = ((flatCold.billedEq - orchCold.billedEq) / flatCold.billedEq) * 100;
  const warmDelta = ((flatWarm.billedEq - orchWarm.billedEq) / flatWarm.billedEq) * 100;
  console.log(`\n  ORCH vs FLAT billed-eq-in delta:  cold ${coldDelta.toFixed(1)}%   warm ${warmDelta.toFixed(1)}%`);
  console.log("  (positive = ORCH cheaper; negative = ORCH more expensive)");

  console.log("\nJSON:");
  console.log(JSON.stringify({ model: MODEL, modules: MODULES.length, flatCold, orchCold, flatWarm, orchWarm }, null, 2));
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
