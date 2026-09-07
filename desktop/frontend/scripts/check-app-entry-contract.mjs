#!/usr/bin/env node
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const file = resolve("src/App.tsx");
const source = readFileSync(file, "utf8");
const lines = source.split(/\r?\n/).length;
const failures = [];
if (lines > 200) failures.push(`App.tsx is ${lines} lines; composition boundary is 200`);
if (/\bapp\./.test(source) || /from ["']\.\/lib\/bridge["']/.test(source)) failures.push("App.tsx directly accesses the Wails bridge");
if (/\buseEffect\s*\(/.test(source) || /\bawait\b/.test(source)) failures.push("App.tsx owns an effect or asynchronous operation");
if (!/from ["']\.\/AppRuntime["']/.test(source)) failures.push("App.tsx must compose AppRuntime");
if (failures.length) {
  for (const failure of failures) console.error(`app-entry-contract: ${failure}`);
  process.exitCode = 1;
} else {
  console.log("app-entry-contract: App.tsx is a pure composition boundary");
}
