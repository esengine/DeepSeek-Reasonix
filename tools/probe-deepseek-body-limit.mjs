#!/usr/bin/env node
// One-shot probe: send progressively larger JSON bodies to DeepSeek's chat
// endpoint to detect the current gateway body-size limit (if any). Validates
// whether MAX_BODY_BYTES in src/context-manager.ts is still load-bearing.
//
// Sends max_tokens=1 to cap output cost; the request is the variable we care
// about. Uses one big user message of plain ASCII filler ('A' chars) so the
// JSON body grows linearly with the message length.

import { readFileSync } from "node:fs";

function loadEnv(path) {
  try {
    const raw = readFileSync(path, "utf8");
    for (const line of raw.split(/\r?\n/)) {
      const m = line.match(/^([A-Z_][A-Z0-9_]*)=(.*)$/);
      if (!m) continue;
      if (process.env[m[1]] === undefined) {
        let val = m[2].trim();
        if (
          (val.startsWith('"') && val.endsWith('"')) ||
          (val.startsWith("'") && val.endsWith("'"))
        ) {
          val = val.slice(1, -1);
        }
        process.env[m[1]] = val;
      }
    }
  } catch {}
}

loadEnv("F:/Reasonix/.env");
const apiKey = process.env.DEEPSEEK_API_KEY;
if (!apiKey) {
  console.error("DEEPSEEK_API_KEY missing");
  process.exit(1);
}
const baseUrl = (process.env.DEEPSEEK_BASE_URL ?? "https://api.deepseek.com").replace(/\/+$/, "");

const SIZES_KB = [100, 500, 700, 800, 884, 1000, 1500, 2000, 3000, 5000, 8000];

async function probe(sizeKB) {
  // Subtract a few hundred bytes to leave room for JSON envelope so the actual
  // wire body lands close to sizeKB * 1024.
  const fillerLen = sizeKB * 1024 - 400;
  const filler = "A".repeat(Math.max(0, fillerLen));
  const body = JSON.stringify({
    model: "deepseek-chat",
    messages: [
      { role: "user", content: `Reply with the single character "ok". ${filler}` },
    ],
    max_tokens: 1,
    stream: false,
  });
  const actualBytes = Buffer.byteLength(body, "utf8");
  const t0 = Date.now();
  let status = 0;
  let snippet = "";
  let err = "";
  try {
    const resp = await fetch(`${baseUrl}/chat/completions`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${apiKey}`,
        "Content-Type": "application/json",
      },
      body,
    });
    status = resp.status;
    const text = await resp.text();
    snippet = text.slice(0, 300).replace(/\s+/g, " ");
  } catch (e) {
    err = String(e).slice(0, 300);
  }
  const dtMs = Date.now() - t0;
  return { sizeKB, actualBytes, status, dtMs, snippet, err };
}

console.log(`Probing ${baseUrl}/chat/completions`);
console.log("sizeKB | bytes      | status | ms    | snippet/error");
console.log("-------|------------|--------|-------|------------------------");
for (const kb of SIZES_KB) {
  const r = await probe(kb);
  const tag = r.err ? `ERR ${r.err}` : `${r.snippet}`;
  console.log(
    `${String(r.sizeKB).padStart(6)} | ${String(r.actualBytes).padStart(10)} | ${
      String(r.status).padStart(6)
    } | ${String(r.dtMs).padStart(5)} | ${tag}`,
  );
}
