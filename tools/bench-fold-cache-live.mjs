#!/usr/bin/env node
// Live cache-hit benchmark for the fold summary path.
//
// Sends the same simulated fold scenario to DeepSeek twice:
//   1. OLD shape (bespoke "compress conversation" system + no tools)
//   2. NEW shape (live agent system + tools)
// Both come AFTER a single "main agent" priming call that warms the prefix.
// Compares prompt_cache_hit_tokens / prompt_cache_miss_tokens between shapes.
//
// Requires DEEPSEEK_API_KEY. Usage:
//   node tools/bench-fold-cache-live.mjs <session.jsonl> [--model deepseek-v4-flash]

import { readFileSync } from "node:fs";

const SYS_OLD =
  "You compress conversation history for a coding agent. Output one prose recap that preserves: " +
  "the user's ORIGINAL OBJECTIVE (never paraphrase away nuance or negative constraints like 'do NOT do X'), " +
  "all 'do not' / 'never' / 'avoid' instructions, decisions and conclusions reached, " +
  "files inspected or modified, important tool results still relevant to ongoing work, " +
  "and any open todos. Skip turn-by-turn play-by-play. No tool calls, no markdown headings, no SEARCH/REPLACE blocks — plain prose only.";

const SUMMARY_INSTRUCTION_OLD =
  "Summarize the conversation above as plain prose. This summary replaces the original turns to free context — make it self-contained.";

const SUMMARY_INSTRUCTION_NEW =
  "Summarize the conversation above as one self-contained prose recap. Preserve the user's " +
  "ORIGINAL OBJECTIVE (never paraphrase away negative constraints like 'do NOT do X'), all " +
  "'do not' / 'never' / 'avoid' instructions, decisions reached, files inspected or modified, " +
  "tool results still relevant, and any open todos. Skip turn-by-turn play-by-play. " +
  "Output plain prose only — no tool calls, no markdown headings, no SEARCH/REPLACE blocks.";

const REPRESENTATIVE_AGENT_SYSTEM =
  "You are a coding agent. Use tools to read files, run commands, and edit code. " +
  "Follow the user's instructions precisely. Prefer minimal changes. " +
  "Do not introduce features beyond what was asked. " +
  "Be concise in user-facing text. Trust internal code; only validate at boundaries.";

const REPRESENTATIVE_TOOLS = [
  {
    type: "function",
    function: {
      name: "Read",
      description: "Read a file from the local filesystem.",
      parameters: {
        type: "object",
        properties: { path: { type: "string", description: "Absolute path" } },
        required: ["path"],
      },
    },
  },
  {
    type: "function",
    function: {
      name: "Bash",
      description: "Execute a bash command.",
      parameters: {
        type: "object",
        properties: { command: { type: "string" } },
        required: ["command"],
      },
    },
  },
  {
    type: "function",
    function: {
      name: "Grep",
      description: "Search file contents with a regex.",
      parameters: {
        type: "object",
        properties: {
          pattern: { type: "string" },
          path: { type: "string" },
          glob: { type: "string" },
        },
        required: ["pattern"],
      },
    },
  },
];

function approxTokens(s) {
  return Math.ceil(s.length / 3.5);
}
function msgTokens(m) {
  const c = typeof m.content === "string" ? m.content : JSON.stringify(m.content ?? "");
  const tc = Array.isArray(m.tool_calls) ? JSON.stringify(m.tool_calls) : "";
  return approxTokens(c) + approxTokens(tc) + 4;
}

function loadSession(path) {
  return readFileSync(path, "utf8")
    .split("\n")
    .filter((l) => l.trim())
    .map((l) => JSON.parse(l));
}

function pickFoldPoint(messages, headRatio = 0.7) {
  const tokens = messages.map(msgTokens);
  const total = tokens.reduce((a, b) => a + b, 0);
  const headBudget = total * headRatio;
  let cum = 0;
  for (let i = 0; i < messages.length; i++) {
    cum += tokens[i];
    if (cum >= headBudget) return { boundary: i + 1, total, headTokens: cum };
  }
  return { boundary: messages.length, total, headTokens: total };
}

// Trim head until every assistant.tool_calls has matching tool messages
// later in head. Walking back from the cut, drop any assistant whose
// tool_calls aren't fully answered within the remaining head — and the
// orphaned tool messages between it and the cut.
function healHead(head) {
  let h = [...head];
  while (h.length > 0) {
    let lastAssistantWithCalls = -1;
    for (let i = h.length - 1; i >= 0; i--) {
      const m = h[i];
      if (m.role === "assistant" && Array.isArray(m.tool_calls) && m.tool_calls.length > 0) {
        lastAssistantWithCalls = i;
        break;
      }
    }
    if (lastAssistantWithCalls < 0) break;
    const asst = h[lastAssistantWithCalls];
    const wanted = new Set(asst.tool_calls.map((c) => c.id));
    const respondedAfter = new Set(
      h.slice(lastAssistantWithCalls + 1)
        .filter((m) => m.role === "tool" && m.tool_call_id)
        .map((m) => m.tool_call_id),
    );
    let allAnswered = true;
    for (const id of wanted) {
      if (!respondedAfter.has(id)) {
        allAnswered = false;
        break;
      }
    }
    if (allAnswered) break;
    h = h.slice(0, lastAssistantWithCalls);
  }
  return h;
}

async function call(apiKey, body) {
  const resp = await fetch("https://api.deepseek.com/chat/completions", {
    method: "POST",
    headers: { Authorization: `Bearer ${apiKey}`, "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!resp.ok) throw new Error(`${resp.status}: ${await resp.text()}`);
  const data = await resp.json();
  return {
    usage: data.usage ?? {},
    contentLen: (data.choices?.[0]?.message?.content ?? "").length,
  };
}

function fmtUsage(label, u) {
  const hit = u.prompt_cache_hit_tokens ?? 0;
  const miss = u.prompt_cache_miss_tokens ?? 0;
  const total = u.prompt_tokens ?? hit + miss;
  const hitRate = total > 0 ? hit / total : 0;
  const PRICE_INPUT = 3.0 / 1e6;
  const PRICE_CACHE = 0.3 / 1e6;
  const PRICE_OUT = 15.0 / 1e6;
  const inputCost = hit * PRICE_CACHE + miss * PRICE_INPUT;
  const outCost = (u.completion_tokens ?? 0) * PRICE_OUT;
  console.log(
    `${label}: prompt=${total.toLocaleString()} (hit ${hit.toLocaleString()}/${(hitRate * 100).toFixed(1)}%, miss ${miss.toLocaleString()}) · out=${u.completion_tokens ?? 0} · input $${inputCost.toFixed(5)} · total $${(inputCost + outCost).toFixed(5)}`,
  );
}

async function main() {
  const args = process.argv.slice(2);
  const path = args[0];
  const modelIdx = args.indexOf("--model");
  const model = modelIdx >= 0 ? args[modelIdx + 1] : "deepseek-v4-flash";
  if (!path) {
    console.error("usage: bench-fold-cache-live.mjs <session.jsonl> [--model deepseek-v4-flash]");
    process.exit(2);
  }
  const apiKey = process.env.DEEPSEEK_API_KEY;
  if (!apiKey) {
    console.error("DEEPSEEK_API_KEY not set");
    process.exit(2);
  }

  const log = loadSession(path);
  const { boundary, total } = pickFoldPoint(log);
  const head = healHead(log.slice(0, boundary));
  const recentMsg = { role: "user", content: "what's next?" };

  console.log(
    `session: ${path}\n  ${log.length} messages · ~${total.toLocaleString()} tok · head=${boundary} msgs (~${head.reduce((a, m) => a + msgTokens(m), 0).toLocaleString()} tok)\n  model: ${model}\n`,
  );

  const baseExtra = { thinking: { type: "disabled" } };

  console.log("=== step 1: PRIMING (main-agent shape) — warms the prefix cache ===");
  const primingBody = {
    model,
    messages: [{ role: "system", content: REPRESENTATIVE_AGENT_SYSTEM }, ...head, recentMsg],
    tools: REPRESENTATIVE_TOOLS,
    stream: false,
    max_tokens: 64,
    extra_body: baseExtra,
  };
  const primingResp = await call(apiKey, primingBody);
  fmtUsage("priming", primingResp.usage);

  console.log("\n=== step 2: OLD summary shape (bespoke system + no tools) ===");
  const oldBody = {
    model,
    messages: [
      { role: "system", content: SYS_OLD },
      ...head,
      { role: "user", content: SUMMARY_INSTRUCTION_OLD },
    ],
    stream: false,
    max_tokens: 256,
    extra_body: baseExtra,
  };
  const oldResp = await call(apiKey, oldBody);
  fmtUsage("OLD    ", oldResp.usage);

  console.log("\n=== step 3: NEW summary shape (live agent system + tools) ===");
  const newBody = {
    model,
    messages: [
      { role: "system", content: REPRESENTATIVE_AGENT_SYSTEM },
      ...head,
      { role: "user", content: SUMMARY_INSTRUCTION_NEW },
    ],
    tools: REPRESENTATIVE_TOOLS,
    stream: false,
    max_tokens: 256,
    extra_body: baseExtra,
  };
  const newResp = await call(apiKey, newBody);
  fmtUsage("NEW    ", newResp.usage);

  const oldHit = oldResp.usage.prompt_cache_hit_tokens ?? 0;
  const oldTot = oldResp.usage.prompt_tokens ?? 0;
  const newHit = newResp.usage.prompt_cache_hit_tokens ?? 0;
  const newTot = newResp.usage.prompt_tokens ?? 0;
  const PRICE_INPUT = 3.0 / 1e6;
  const PRICE_CACHE = 0.3 / 1e6;
  const oldCost = oldHit * PRICE_CACHE + (oldTot - oldHit) * PRICE_INPUT;
  const newCost = newHit * PRICE_CACHE + (newTot - newHit) * PRICE_INPUT;

  console.log("\n=== verdict ===");
  console.log(
    `OLD cache hit: ${((oldHit / Math.max(oldTot, 1)) * 100).toFixed(1)}%  → input $${oldCost.toFixed(5)}`,
  );
  console.log(
    `NEW cache hit: ${((newHit / Math.max(newTot, 1)) * 100).toFixed(1)}%  → input $${newCost.toFixed(5)}`,
  );
  console.log(
    `saving per fold: $${(oldCost - newCost).toFixed(5)} (${(((oldCost - newCost) / Math.max(oldCost, 1e-12)) * 100).toFixed(1)}%)`,
  );
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
