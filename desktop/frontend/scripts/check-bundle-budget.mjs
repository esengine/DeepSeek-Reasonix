import { readFileSync, readdirSync, statSync } from "node:fs";
import { basename, resolve } from "node:path";
import { gzipSync } from "node:zlib";

const distDir = resolve("dist");
const indexPath = resolve(distDir, "index.html");
const html = readFileSync(indexPath, "utf8");

function gzipBytes(path) {
  return gzipSync(readFileSync(path), { level: 9 }).byteLength;
}

function initialAssetPaths(extension) {
  const pattern = extension === ".js"
    ? /<(?:script|link)\b[^>]+(?:src|href)=["']([^"']+\.js)["'][^>]*>/g
    : /<link\b[^>]+href=["']([^"']+\.css)["'][^>]*>/g;
  return [...new Set([...html.matchAll(pattern)].map((match) => resolve(distDir, match[1])))];
}

function formatKiB(bytes) {
  return `${(bytes / 1024).toFixed(1)} KiB`;
}

function assertBudget(label, actual, budget) {
  if (actual > budget) {
    throw new Error(`${label} is ${formatKiB(actual)}; budget is ${formatKiB(budget)}`);
  }
  process.stdout.write(`  PASS  ${label}: ${formatKiB(actual)} / ${formatKiB(budget)}\n`);
}

const initialJS = initialAssetPaths(".js");
const initialCSS = initialAssetPaths(".css");
if (!initialJS.length) throw new Error("no initial JavaScript assets found in dist/index.html");
if (!initialCSS.length) throw new Error("no initial CSS assets found in dist/index.html");

// main.tsx intentionally loads styles.css before mounting React so the inline
// boot shell can paint without waiting for the full application stylesheet.
// Vite emits that entry as styles-<hash>.css; keep it in the startup budget
// while also proving it never drifts back into the render-blocking HTML path.
const appShellCSS = readdirSync(resolve(distDir, "assets"))
  .filter((name) => /^styles-.+\.css$/.test(name))
  .map((name) => resolve(distDir, "assets", name));
if (appShellCSS.length !== 1) {
  throw new Error(`expected exactly one deferred app-shell stylesheet, found ${appShellCSS.length}`);
}
if (initialCSS.some((path) => appShellCSS.includes(path))) {
  throw new Error("app-shell stylesheet must not block the inline boot shell's first paint");
}

const initialJSGzip = initialJS.reduce((total, path) => total + gzipBytes(path), 0);
const initialCSSGzip = initialCSS.reduce((total, path) => total + gzipBytes(path), 0);
const appShellCSSGzip = appShellCSS.reduce((total, path) => total + gzipBytes(path), 0);
const largestInitialJS = Math.max(...initialJS.map(gzipBytes));
const largestInitialJSRaw = Math.max(...initialJS.map((path) => statSync(path).size));
const localeChunks = readdirSync(resolve(distDir, "assets"))
  .filter((name) => /^(?:zh|zh-TW)-.+\.js$/.test(name))
  .map((name) => resolve(distDir, "assets", name));

console.log("\nbundle budgets");
// The merged execution-setting controller adds 1.1 KiB gzip (0.27%) over the
// 400.8 KiB base while keeping the interaction on the existing startup path.
assertBudget("initial JavaScript gzip", initialJSGzip, 402 * 1024);
assertBudget("largest initial JavaScript chunk gzip", largestInitialJS, 280 * 1024);
assertBudget("render-blocking CSS gzip", initialCSSGzip, 4 * 1024);
// Extension surfaces, Task Monitor, and compact decision receipts share the
// application stylesheet loaded before React mounts. Keep their combined
// allowance bounded even though the file is no longer render-blocking.
assertBudget("deferred app-shell CSS gzip", appShellCSSGzip, 114 * 1024);
if (localeChunks.length !== 2) {
  throw new Error(`expected 2 on-demand Chinese locale chunks, found ${localeChunks.length}`);
}
for (const path of localeChunks) {
  const name = basename(path);
  // Task Monitor, billing, indexed history, Task Center, Extension UI, and
  // runtime controls plus execution-setting receipts add localized copy. Keep
  // both dictionaries bounded.
  const budget = name.startsWith("zh-TW-") ? 55.5 * 1024 : 54.5 * 1024;
  assertBudget(`${name} gzip`, gzipBytes(path), budget);
}

const rawInitialBytes = [...initialJS, ...initialCSS, ...appShellCSS]
  .reduce((total, path) => total + statSync(path).size, 0);
// Native Web Animations and frame-batched scrolling avoid an eager animation
// runtime. Goal request observability plus transcript scroll arbitration,
// logical selection state/DOM adapters, native input-session ownership,
// durable inbox recovery, indexed catalogs, Task Center, structured billing,
// startup config warnings, hover-revealed turn-action labels, and compact
// execution-setting receipts add small always-available contracts. Keep the
// raw allowance ratcheted while gzip startup budgets stay flat.
// The same contract adds 4.2 KiB raw (0.19%) over the 2,264.0 KiB base.
assertBudget("initial raw JavaScript and CSS", rawInitialBytes, 2_268.5 * 1024);
assertBudget("largest initial JavaScript chunk raw", largestInitialJSRaw, 1_000 * 1024);
