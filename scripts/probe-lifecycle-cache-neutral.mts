/**
 * Live DeepSeek cache probe for the runtime-only Engineering Lifecycle design.
 *
 * This is intentionally NOT wired into CI. It needs DEEPSEEK_API_KEY and the
 * live provider cache. Run manually when validating cache neutrality:
 *
 *   npx tsx scripts/probe-lifecycle-cache-neutral.mts
 *
 * The deterministic invariant lives in tests/code-prompt.test.ts. This probe
 * checks the economic side: off/strict prompts are byte-identical, and warm
 * turns should report >=99% prompt-cache hit after the cold start.
 */

import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { codeSystemPrompt } from "../src/code/prompt.js";

function loadDotenv(path: string): void {
  try {
    const txt = readFileSync(path, "utf8");
    for (const line of txt.split(/\r?\n/)) {
      const m = line.match(/^([A-Z_][A-Z0-9_]*)=(.*)$/);
      if (m && !process.env[m[1]]) process.env[m[1]] = m[2];
    }
  } catch {
    /* optional */
  }
}

loadDotenv("./.env.testbak");

const KEY = process.env.DEEPSEEK_API_KEY;
const BASE = process.env.DEEPSEEK_BASE_URL ?? "https://api.deepseek.com";
const MODEL = process.env.REASONIX_CACHE_PROBE_MODEL ?? "deepseek-v4-flash";

if (!KEY) {
  throw new Error("DEEPSEEK_API_KEY missing; set it or add .env.testbak.");
}

const filler = (label: string, n: number): string =>
  Array.from(
    { length: n },
    (_, i) =>
      `${label} line ${i}: cache-neutral lifecycle probe context with stable deterministic bytes for DeepSeek prefix-cache measurement.`,
  ).join("\n");

function messages(system: string, turn: number) {
  const out: Array<{ role: "system" | "user" | "assistant"; content: string }> = [
    { role: "system", content: system },
    { role: "user", content: `seed block A:\n${filler("A", 80)}` },
    { role: "assistant", content: "seed A acknowledged." },
    { role: "user", content: `seed block B:\n${filler("B", 80)}` },
    { role: "assistant", content: "seed B acknowledged." },
  ];
  for (let i = 0; i < turn; i++) {
    out.push({ role: "user", content: `historical turn ${i}: ${filler(`H${i}`, 12)}` });
    out.push({ role: "assistant", content: `historical answer ${i}: ok.` });
  }
  out.push({ role: "user", content: `probe turn ${turn}: reply with exactly "ok ${turn}".` });
  return out;
}

async function call(label: string, system: string, turn: number): Promise<number> {
  const res = await fetch(`${BASE}/chat/completions`, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      authorization: `Bearer ${KEY}`,
    },
    body: JSON.stringify({
      model: MODEL,
      messages: messages(system, turn),
      temperature: 0,
      max_tokens: 8,
      stream: false,
    }),
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${label}: HTTP ${res.status}: ${text.slice(0, 400)}`);
  }
  const json = (await res.json()) as {
    usage?: {
      prompt_tokens?: number;
      prompt_cache_hit_tokens?: number;
      prompt_cache_miss_tokens?: number;
    };
  };
  const usage = json.usage ?? {};
  const hit = usage.prompt_cache_hit_tokens ?? 0;
  const miss = usage.prompt_cache_miss_tokens ?? Math.max(0, (usage.prompt_tokens ?? 0) - hit);
  const ratio = hit + miss > 0 ? hit / (hit + miss) : 0;
  console.log(
    `${label}: prompt=${usage.prompt_tokens ?? 0} hit=${hit} miss=${miss} hit%=${(ratio * 100).toFixed(2)}`,
  );
  return ratio;
}

async function main(): Promise<void> {
  const root = mkdtempSync(join(tmpdir(), "reasonix-cache-neutral-"));
  try {
    const offSystem = codeSystemPrompt(root, { engineeringLifecycleMode: "off" });
    const strictSystem = codeSystemPrompt(root, { engineeringLifecycleMode: "strict" });
    if (offSystem !== strictSystem) {
      throw new Error("off/strict system prompts differ; runtime-only cache invariant is broken.");
    }

    const ratios: number[] = [];
    for (let turn = 0; turn < 5; turn++) {
      const offRatio = await call(`off-${turn}`, offSystem, turn);
      const strictRatio = await call(`strict-${turn}`, strictSystem, turn);
      if (turn > 0) ratios.push(offRatio, strictRatio);
      await new Promise((resolve) => setTimeout(resolve, 1200));
    }

    const avg = ratios.reduce((sum, value) => sum + value, 0) / ratios.length;
    console.log(`warm average hit%=${(avg * 100).toFixed(2)}`);
    if (avg < 0.99) {
      throw new Error(`warm cache hit below target: ${(avg * 100).toFixed(2)}% < 99%`);
    }
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
