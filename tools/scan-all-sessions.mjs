#!/usr/bin/env node
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

const dir = process.argv[2] ?? "C:/Users/yuhuahui/.reasonix/sessions";
const files = readdirSync(dir).filter(
  (f) => f.endsWith(".jsonl") && !f.endsWith(".events.jsonl") && !f.startsWith("subagent-"),
);

const loneSurrogateRegex =
  /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g;

const rows = [];
for (const f of files) {
  const path = join(dir, f);
  const fsize = statSync(path).size;
  if (fsize < 50_000) continue;

  let raw;
  try {
    raw = readFileSync(path, "utf8");
  } catch {
    continue;
  }

  const lines = raw.split("\n").filter((l) => l.trim());
  let messages;
  try {
    messages = lines.map((l) => JSON.parse(l));
  } catch {
    continue;
  }

  const body = JSON.stringify({ model: "deepseek-v4-flash", messages, stream: true });
  const bodyBytes = Buffer.byteLength(body, "utf8");
  const loneSurrogates = (body.match(loneSurrogateRegex) ?? []).length;

  let maxMsgBytes = 0;
  let maxMsgRole = "";
  let maxMsgIdx = -1;
  for (let i = 0; i < messages.length; i++) {
    const mb = Buffer.byteLength(JSON.stringify(messages[i]), "utf8");
    if (mb > maxMsgBytes) {
      maxMsgBytes = mb;
      maxMsgRole = messages[i].role;
      maxMsgIdx = i;
    }
  }

  rows.push({
    file: f.slice(0, 50),
    bodyKB: Math.round(bodyBytes / 1024),
    msgs: messages.length,
    maxMsgKB: Math.round(maxMsgBytes / 1024),
    maxRole: maxMsgRole,
    maxIdx: maxMsgIdx,
    surrogates: loneSurrogates,
  });
}

rows.sort((a, b) => b.bodyKB - a.bodyKB);
console.log(`Scanned ${rows.length} sessions ≥ 50 KB:\n`);
console.table(rows.slice(0, 12));

const overLimit = rows.filter((r) => r.bodyKB > 500);
if (overLimit.length > 0) {
  console.log(`\n${overLimit.length} sessions with body > 500 KB:`);
  for (const r of overLimit) {
    console.log(`  ${r.bodyKB}KB - ${r.msgs} msgs - biggest msg ${r.maxMsgKB}KB (${r.maxRole}#${r.maxIdx}) - surrogates: ${r.surrogates}`);
  }
}

const anyWithSurrogates = rows.filter((r) => r.surrogates > 0);
if (anyWithSurrogates.length > 0) {
  console.log(`\n${anyWithSurrogates.length} sessions with lone surrogates:`);
  for (const r of anyWithSurrogates) {
    console.log(`  ${r.surrogates} surrogates in ${r.file}`);
  }
} else {
  console.log("\nNo lone surrogates found across any session — H3 weak.");
}
