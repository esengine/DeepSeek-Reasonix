#!/usr/bin/env node
// Local cache-shape benchmark: takes a real session .jsonl, simulates the OLD
// and NEW summary-call request shapes at a plausible fold point, then reports
// how many leading messages (and roughly how many tokens) each shape shares
// with the main agent's preceding request. DeepSeek's prefix cache hits up to
// the first message that differs, so message-level shared prefix is the upper
// bound on cache hit on that summary call.

import { readFileSync, statSync } from "node:fs";

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
    if (cum >= headBudget) {
      return { boundary: i + 1, total, headTokens: cum };
    }
  }
  return { boundary: messages.length, total, headTokens: total };
}

function sharedLeadingMessages(a, b) {
  const n = Math.min(a.length, b.length);
  for (let i = 0; i < n; i++) {
    if (JSON.stringify(a[i]) !== JSON.stringify(b[i])) return i;
  }
  return n;
}

function tokenCountOf(messages) {
  return messages.reduce((a, m) => a + msgTokens(m), 0);
}

function bench(path, agentSystem) {
  const log = loadSession(path);
  if (log.length < 10) return null;
  const { boundary, total, headTokens } = pickFoldPoint(log);
  const head = log.slice(0, boundary);
  const recentMsg = log[boundary] ?? { role: "user", content: "next instruction" };

  const mainAgentMsgs = [{ role: "system", content: agentSystem }, ...head, recentMsg];

  const oldSummaryMsgs = [
    { role: "system", content: SYS_OLD },
    ...head,
    { role: "user", content: SUMMARY_INSTRUCTION_OLD },
  ];

  const newSummaryMsgs = [
    { role: "system", content: agentSystem },
    ...head,
    { role: "user", content: SUMMARY_INSTRUCTION_NEW },
  ];

  const oldShared = sharedLeadingMessages(mainAgentMsgs, oldSummaryMsgs);
  const newShared = sharedLeadingMessages(mainAgentMsgs, newSummaryMsgs);

  const oldCacheTokens = tokenCountOf(oldSummaryMsgs.slice(0, oldShared));
  const newCacheTokens = tokenCountOf(newSummaryMsgs.slice(0, newShared));
  const oldTotalTokens = tokenCountOf(oldSummaryMsgs);
  const newTotalTokens = tokenCountOf(newSummaryMsgs);

  return {
    file: path,
    sizeKB: Math.round(statSync(path).size / 1024),
    logMessages: log.length,
    foldBoundary: boundary,
    totalLogTokens: total,
    headTokens,
    OLD: {
      sharedMessages: oldShared,
      cacheableTokens: oldCacheTokens,
      totalRequestTokens: oldTotalTokens,
      hitRatio: oldTotalTokens > 0 ? oldCacheTokens / oldTotalTokens : 0,
    },
    NEW: {
      sharedMessages: newShared,
      cacheableTokens: newCacheTokens,
      totalRequestTokens: newTotalTokens,
      hitRatio: newTotalTokens > 0 ? newCacheTokens / newTotalTokens : 0,
    },
  };
}

function fmtTokens(n) {
  if (n >= 1000) return (n / 1000).toFixed(1) + "k";
  return String(n);
}

function fmtPct(r) {
  return (r * 100).toFixed(1) + "%";
}

const PRICE_INPUT = 3.0 / 1e6;
const PRICE_CACHE = 0.30 / 1e6;

function costOf(r) {
  return r.cacheableTokens * PRICE_CACHE + (r.totalRequestTokens - r.cacheableTokens) * PRICE_INPUT;
}

const files = process.argv.slice(2);
if (files.length === 0) {
  console.error("usage: bench-fold-cache-shape.mjs <session.jsonl> [more.jsonl ...]");
  process.exit(2);
}

const REPRESENTATIVE_SYSTEM = "x".repeat(6000);

const results = files.map((f) => bench(f, REPRESENTATIVE_SYSTEM)).filter(Boolean);

console.log("\n=== Per-session fold cache shape ===");
for (const r of results) {
  console.log(`\n${r.file}`);
  console.log(
    `  ${r.logMessages} messages · total ${fmtTokens(r.totalLogTokens)} tok · head ${fmtTokens(r.headTokens)} tok (fold at msg ${r.foldBoundary})`,
  );
  console.log(
    `  OLD shape: shared ${r.OLD.sharedMessages} leading msgs → cache hit ${fmtTokens(r.OLD.cacheableTokens)} / ${fmtTokens(r.OLD.totalRequestTokens)} tok = ${fmtPct(r.OLD.hitRatio)}  → $${costOf(r.OLD).toFixed(5)}`,
  );
  console.log(
    `  NEW shape: shared ${r.NEW.sharedMessages} leading msgs → cache hit ${fmtTokens(r.NEW.cacheableTokens)} / ${fmtTokens(r.NEW.totalRequestTokens)} tok = ${fmtPct(r.NEW.hitRatio)}  → $${costOf(r.NEW).toFixed(5)}`,
  );
  const savings = costOf(r.OLD) - costOf(r.NEW);
  const savingPct = costOf(r.OLD) > 0 ? savings / costOf(r.OLD) : 0;
  console.log(`  SAVING per fold: $${savings.toFixed(5)} (${fmtPct(savingPct)})`);
}

console.log("\n=== Aggregate ===");
const oldTotal = results.reduce((a, r) => a + costOf(r.OLD), 0);
const newTotal = results.reduce((a, r) => a + costOf(r.NEW), 0);
const oldCacheTok = results.reduce((a, r) => a + r.OLD.cacheableTokens, 0);
const newCacheTok = results.reduce((a, r) => a + r.NEW.cacheableTokens, 0);
const oldReqTok = results.reduce((a, r) => a + r.OLD.totalRequestTokens, 0);
const newReqTok = results.reduce((a, r) => a + r.NEW.totalRequestTokens, 0);
console.log(`folds simulated: ${results.length}`);
console.log(
  `OLD: ${fmtTokens(oldCacheTok)} cached / ${fmtTokens(oldReqTok)} sent = ${fmtPct(oldCacheTok / oldReqTok)} hit · total $${oldTotal.toFixed(4)}`,
);
console.log(
  `NEW: ${fmtTokens(newCacheTok)} cached / ${fmtTokens(newReqTok)} sent = ${fmtPct(newCacheTok / newReqTok)} hit · total $${newTotal.toFixed(4)}`,
);
console.log(
  `aggregate saving: $${(oldTotal - newTotal).toFixed(4)} (${fmtPct((oldTotal - newTotal) / oldTotal)})`,
);
