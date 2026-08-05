#!/usr/bin/env node
// Fails when a test prints a credential that is not one of its own fixtures.
//
// A test suite may print sk-legacy-123 because it wrote it a line earlier. It
// may not print a value that exists only in the developer's real credential
// store — that means the suite reached past its sandbox. The difference between
// the two is whether the value appears anywhere in the test sources.
//
//   node scripts/credential-leak-check.mjs [./package/...]
//
// Exit 0 clean, 1 leaked. Values are never printed; a short digest identifies
// them across runs so a fix can be verified against the same finding.

import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { createHash } from "node:crypto";

const PKG = process.argv[2] ?? "./internal/config/";

// Anything shaped like a provider token. Deliberately loose: a false positive
// costs one look at the fixtures, a false negative costs a leaked key.
const SECRET = /\b(?:sk|pk|api|key|token)[-_][A-Za-z0-9_-]{6,}\b/gi;

const digest = (s) => createHash("sha256").update(s).digest("hex").slice(0, 8);
const redact = (s) => `${s.slice(0, 3)}…${s.length} chars, sha256:${digest(s)}`;

function collectSources(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) {
      if (name !== "node_modules" && name !== ".git") collectSources(p, out);
    } else if (/\.(go|ts|tsx|js|mjs|json|toml|ya?ml)$/.test(name)) {
      out.push(p);
    }
  }
  return out;
}

// Everything written down in the repo is a fixture by definition — it cannot
// have come from the machine running the tests.
const fixtures = new Set();
for (const file of collectSources(".")) {
  let text;
  try {
    text = readFileSync(file, "utf8");
  } catch {
    continue;
  }
  for (const m of text.matchAll(SECRET)) fixtures.add(m[0]);
}

let output = "";
try {
  output = execFileSync("go", ["test", PKG, "-count=1"], {
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
    stdio: ["ignore", "pipe", "pipe"],
  });
} catch (err) {
  output = `${err.stdout ?? ""}${err.stderr ?? ""}`;
}

const leaked = new Map();
for (const line of output.split("\n")) {
  for (const m of line.matchAll(SECRET)) {
    if (fixtures.has(m[0])) continue;
    if (!leaked.has(m[0])) leaked.set(m[0], line.trim().slice(0, 120));
  }
}

console.log(`package:  ${PKG}`);
console.log(`fixtures: ${fixtures.size} credential-shaped literals found in repo sources`);

if (leaked.size === 0) {
  console.log("\nCLEAN — every credential printed by the suite is one of its own fixtures.");
  process.exit(0);
}

console.log(`\nLEAKED — ${leaked.size} value(s) printed that exist nowhere in the sources:\n`);
for (const [value, context] of leaked) {
  console.log(`  ${redact(value)}`);
  console.log(`    context: ${context.replace(value, "<redacted>")}\n`);
}
console.log("A value the suite did not write can only have come from the machine's");
console.log("real credential store, which means the test escaped its sandbox.");
process.exit(1);
