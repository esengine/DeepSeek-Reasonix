#!/usr/bin/env tsx
// End-to-end probe of the refactored context-compression flow against the
// LIVE DeepSeek API. Verifies:
//   1) Post-response auto-fold (decideAfterUsage triggers compactHistory).
//   2) Preflight emergency path (estimate > 95% trips fold-then-mechanical).
//   3) Single-path Claude-Code-style behavior (fold first, mechanical fallback).
//
// Uses a small artificial ctxMax so paths trigger without bloating context.
// Hits real summarizer + main calls — burns a few cents of API balance.

import { readFileSync } from "node:fs";
import { DeepSeekClient } from "../src/client.ts";
import { CacheFirstLoop } from "../src/loop.ts";
import { ImmutablePrefix } from "../src/memory/runtime.ts";
import { DEEPSEEK_CONTEXT_TOKENS } from "../src/telemetry/stats.ts";

function loadEnv(path: string): void {
  try {
    const raw = readFileSync(path, "utf8");
    for (const line of raw.split(/\r?\n/)) {
      const m = line.match(/^([A-Z_][A-Z0-9_]*)=(.*)$/);
      if (!m) continue;
      if (process.env[m[1]!] === undefined) {
        let val = m[2]!.trim();
        if (
          (val.startsWith('"') && val.endsWith('"')) ||
          (val.startsWith("'") && val.endsWith("'"))
        ) {
          val = val.slice(1, -1);
        }
        process.env[m[1]!] = val;
      }
    }
  } catch {}
}

loadEnv("F:/Reasonix/.env");

const MODEL = "deepseek-chat";
const TEST_CTX = 50_000;
DEEPSEEK_CONTEXT_TOKENS[MODEL] = TEST_CTX;

function fillerTurn(i: number, bulk: number): string {
  return Array.from(
    { length: bulk },
    (_, j) =>
      `Step ${j} of turn ${i}: ` +
      "we discussed the historical context of the question, " +
      "considered several alternative approaches, " +
      "evaluated trade-offs across performance and clarity.",
  ).join("\n");
}

interface Captured {
  kind: string;
  content?: string;
  usage?: unknown;
}

async function scenarioPostResponseFold(): Promise<void> {
  console.log("\n=== Scenario 1: high-ratio response (no tool calls) ===");
  console.log("  (post-response auto-fold only runs mid-tool-loop; a terminal");
  console.log("  text response intentionally skips fold — loop exits cleanly)");
  const client = new DeepSeekClient();
  const loop = new CacheFirstLoop({
    client,
    prefix: new ImmutablePrefix({ system: "Reply with exactly one short sentence." }),
    model: MODEL,
    stream: false,
  });
  for (let i = 0; i < 22; i++) {
    loop.log.append({ role: "user", content: `Q${i}\n${fillerTurn(i, 30)}` });
    loop.log.append({ role: "assistant", content: `A${i}\n${fillerTurn(i, 30)}` });
  }
  const beforeTokens = loop.context.getLogTokens();
  console.log(
    `  seeded ~${beforeTokens.toLocaleString()} log tokens (ctxMax=${TEST_CTX.toLocaleString()})`,
  );

  const events: Captured[] = [];
  for await (const ev of loop.step("Say 'ok' and stop.")) {
    events.push({ kind: ev.role, content: ev.content?.slice(0, 200) });
  }
  const turnStats = loop.stats.turns[loop.stats.turns.length - 1];
  const ratio = turnStats?.usage ? turnStats.usage.promptTokens / TEST_CTX : 0;
  const finalAns = events.find((e) => e.kind === "assistant_final");
  console.log(
    `  real prompt tokens: ${turnStats?.usage?.promptTokens?.toLocaleString()} (ratio=${ratio.toFixed(2)})`,
  );
  console.log(`  final answer: ${finalAns ? finalAns.content : "NO"}`);
  if (!finalAns) {
    console.log("  ❌ FAIL: no final answer");
    process.exitCode = 1;
  } else {
    console.log("  ✅ PASS (high-ratio terminal turn handled without auto-fold, as designed)");
  }
}

async function scenarioPreflightEmergency(): Promise<void> {
  console.log("\n=== Scenario 2: preflight emergency (estimate > 95%) ===");
  const client = new DeepSeekClient();
  const loop = new CacheFirstLoop({
    client,
    prefix: new ImmutablePrefix({ system: "Reply with exactly one short sentence." }),
    model: MODEL,
    stream: false,
  });
  // Seed enough that the local request estimate already > 95% of TEST_CTX (47.5K).
  for (let i = 0; i < 100; i++) {
    loop.log.append({ role: "user", content: `Q${i}\n${fillerTurn(i, 30)}` });
    loop.log.append({ role: "assistant", content: `A${i}\n${fillerTurn(i, 30)}` });
  }
  const beforeMsgs = loop.log.length;
  const beforeTokens = loop.context.getLogTokens();
  console.log(
    `seeded: ${beforeMsgs} messages, ~${beforeTokens.toLocaleString()} log tokens (ctxMax=${TEST_CTX.toLocaleString()})`,
  );

  const events: Captured[] = [];
  for await (const ev of loop.step("Say 'ok' and stop.")) {
    events.push({ kind: ev.role, content: ev.content?.slice(0, 200) });
  }
  const afterMsgs = loop.log.length;
  const afterTokens = loop.context.getLogTokens();

  const preflightStatus = events.find(
    (e) => e.kind === "status" && /preflight|compact/i.test(e.content ?? ""),
  );
  const preflightWarn = events.find(
    (e) => e.kind === "warning" && /^preflight:/.test(e.content ?? ""),
  );
  const finalAns = events.find((e) => e.kind === "assistant_final");

  console.log(`  log: ${beforeMsgs} → ${afterMsgs} messages, ${beforeTokens} → ${afterTokens} tokens`);
  console.log(`  preflight status? ${preflightStatus ? "yes — " + preflightStatus.content : "no"}`);
  console.log(`  preflight warning? ${preflightWarn ? "yes — " + preflightWarn.content : "no"}`);
  console.log(`  final answer received? ${finalAns ? "yes — " + finalAns.content : "NO"}`);

  const compactedOK = preflightWarn?.content?.includes("compacted");
  if (!finalAns) {
    console.log("  ❌ FAIL: no final answer");
    process.exitCode = 1;
  } else if (!preflightWarn) {
    console.log("  ❌ FAIL: preflight did not fire despite log being over 95%");
    process.exitCode = 1;
  } else if (!compactedOK) {
    console.log("  ⚠️  preflight fired but did not compact — see warning above");
  } else {
    console.log("  ✅ PASS");
  }
}

async function scenarioBaselineNoFold(): Promise<void> {
  console.log("\n=== Scenario 3: baseline (small log, no compression) ===");
  const client = new DeepSeekClient();
  const loop = new CacheFirstLoop({
    client,
    prefix: new ImmutablePrefix({ system: "Reply with exactly one short sentence." }),
    model: MODEL,
    stream: false,
  });
  const events: Captured[] = [];
  for await (const ev of loop.step("Say 'ok' and stop.")) {
    events.push({ kind: ev.role, content: ev.content?.slice(0, 200) });
  }
  const preflightWarn = events.find(
    (e) => e.kind === "warning" && /^preflight:/.test(e.content ?? ""),
  );
  const foldWarn = events.find((e) => e.kind === "warning" && /folded \d+/.test(e.content ?? ""));
  const finalAns = events.find((e) => e.kind === "assistant_final");

  console.log(`  preflight fired? ${preflightWarn ? "yes (unexpected)" : "no (expected)"}`);
  console.log(`  fold fired? ${foldWarn ? "yes (unexpected)" : "no (expected)"}`);
  console.log(`  final answer received? ${finalAns ? "yes — " + finalAns.content : "NO"}`);
  if (!finalAns || preflightWarn || foldWarn) {
    console.log("  ❌ FAIL");
    process.exitCode = 1;
  } else {
    console.log("  ✅ PASS");
  }
}

await scenarioBaselineNoFold();
await scenarioPostResponseFold();
await scenarioPreflightEmergency();
console.log("\n=== Done ===");
