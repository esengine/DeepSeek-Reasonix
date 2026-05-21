#!/usr/bin/env node
import { readFileSync } from "node:fs";

const sessionFile = process.argv[2];
if (!sessionFile) {
  console.error("usage: analyze-session-body.mjs <session.jsonl>");
  process.exit(1);
}

const raw = readFileSync(sessionFile, "utf8");
const lines = raw.split("\n").filter((l) => l.trim());
const messages = lines.map((l) => JSON.parse(l));

const payload = {
  model: "deepseek-v4-flash",
  messages,
  stream: true,
};
const body = JSON.stringify(payload);
const bodyBytes = Buffer.byteLength(body, "utf8");
const bodyChars = body.length;

const loneSurrogateRegex =
  /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g;
const loneSurrogates = body.match(loneSurrogateRegex) ?? [];

const perMsg = messages.map((m, i) => {
  const ms = JSON.stringify(m);
  return {
    i,
    role: m.role,
    contentBytes: typeof m.content === "string" ? Buffer.byteLength(m.content, "utf8") : 0,
    fullBytes: Buffer.byteLength(ms, "utf8"),
    toolCalls: Array.isArray(m.tool_calls) ? m.tool_calls.length : 0,
    hasReasoning: typeof m.reasoning_content === "string" && m.reasoning_content.length > 0,
    reasoningBytes:
      typeof m.reasoning_content === "string"
        ? Buffer.byteLength(m.reasoning_content, "utf8")
        : 0,
  };
});

console.log("=== body ===");
console.log(`bytes: ${bodyBytes.toLocaleString()} (${(bodyBytes / 1024).toFixed(1)} KB)`);
console.log(`chars: ${bodyChars.toLocaleString()}`);
console.log(`messages: ${messages.length}`);
console.log(`lone surrogates in body: ${loneSurrogates.length}`);

console.log("\n=== top 8 biggest messages ===");
const top = [...perMsg].sort((a, b) => b.fullBytes - a.fullBytes).slice(0, 8);
console.table(top);

console.log("\n=== role distribution by bytes ===");
const byRole = {};
for (const m of perMsg) {
  byRole[m.role] = (byRole[m.role] || 0) + m.fullBytes;
}
console.table(byRole);

console.log("\n=== messages with reasoning_content ===");
const withReason = perMsg.filter((m) => m.hasReasoning);
console.log(`count: ${withReason.length}`);
if (withReason.length > 0) {
  const totalReasoning = withReason.reduce((s, m) => s + m.reasoningBytes, 0);
  console.log(`total reasoning bytes: ${totalReasoning.toLocaleString()} (${(totalReasoning / 1024).toFixed(1)} KB)`);
  console.log(
    `top 3 reasoning: ${withReason
      .sort((a, b) => b.reasoningBytes - a.reasoningBytes)
      .slice(0, 3)
      .map((m) => `[${m.i}/${m.role}/${(m.reasoningBytes / 1024).toFixed(1)}KB]`)
      .join(" ")}`,
  );
}
