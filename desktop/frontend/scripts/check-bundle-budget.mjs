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

const initialJSGzip = initialJS.reduce((total, path) => total + gzipBytes(path), 0);
const initialCSSGzip = initialCSS.reduce((total, path) => total + gzipBytes(path), 0);
const largestInitialJS = Math.max(...initialJS.map(gzipBytes));
const largestInitialJSRaw = Math.max(...initialJS.map((path) => statSync(path).size));
const localeChunks = readdirSync(resolve(distDir, "assets"))
  .filter((name) => /^(?:zh|zh-TW)-.+\.js$/.test(name))
  .map((name) => resolve(distDir, "assets", name));

console.log("\nbundle budgets");
assertBudget("initial JavaScript gzip", initialJSGzip, 430 * 1024);
assertBudget("largest initial JavaScript chunk gzip", largestInitialJS, 295 * 1024);
// Extension surfaces, Task Monitor, and compact decision receipts share the
// always-loaded shell. Keep their combined allowance bounded to 113 KiB gzip.
assertBudget("initial CSS gzip", initialCSSGzip, 113 * 1024);
if (localeChunks.length !== 2) {
  throw new Error(`expected 2 on-demand Chinese locale chunks, found ${localeChunks.length}`);
}
for (const path of localeChunks) {
  const name = basename(path);
  // Task Monitor adds 37 user-facing labels to each on-demand locale, while
  // Extension UI and Storage & paths add their own status, action, and
  // accessibility copy. Shell execution contract cards add a small set of
  // verification/risk strings. Keep both dictionaries within narrowly
  // measured, explicit allowances.
  const budget = name.startsWith("zh-TW-") ? 53.75 * 1024 : 53 * 1024;
  assertBudget(`${name} gzip`, gzipBytes(path), budget);
}

const rawInitialBytes = [...initialJS, ...initialCSS].reduce((total, path) => total + statSync(path).size, 0);
// Extension UI adds always-loaded cards, status chips, and palette styles;
// Task Monitor adds a small always-loaded style surface while its panel code
// remains lazy-loaded. The gzip budgets above remain the user-visible transfer
// constraint, with a narrowly measured raw allowance for both surfaces.
assertBudget("initial raw JavaScript and CSS", rawInitialBytes, 2_270 * 1024);
assertBudget("largest initial JavaScript chunk raw", largestInitialJSRaw, 1_100 * 1024);
