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
// initial CSS 允许为空：styles.css 走 ?url 延迟加载，feature 样式走 lazy chunk。

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
// React Virtuoso replaces the transcript's custom measurement/anchor engine.
// Its production runtime adds 16.9 KiB gzip (4.2%) over the 402 KiB baseline.
// This exceptional overrun is locally attributable and trades ~1400 lines of
// competing state machines for a maintained library. Native-tail finish helpers
// then sat on the 423.5 KiB gate (Windows CI: 423.5 / 423.5); this 0.5 KiB
// raise (0.12%) absorbs that leave-cancel / remasure-once code without
// widening the original Virtuoso exception. The project-tree archive race
// guards add 611 bytes gzip over main-v2's 423.988 KiB startup path after the
// blank-project flow landed; project-topic sort invalidation and request
// ordering add another bounded 0.2 KiB. Retain both owner boundaries with a
// narrowly rounded 1 KiB ratchet.
// Diagnostic builds intentionally keep content-free row geometry and scroll
// transition probes in the initial transcript path. Stable builds retain the
// existing production ratchet. Per-row measurement versions and a bounded
// recovery probe add less than 0.1% gzip; retain them with a 0.5 KiB (0.118%)
// production ratchet rather than weakening either recovery contract. The
// bounded allowance also covers small gzip drift from the embedded build SHA.
// Reader extent stabilization adds 1.2 KiB gzip (0.28%) in production for its
// bounded input, collapse, rebound, and ownership transaction. Retain it with
// a 1.5 KiB (0.35%) ratchet instead of weakening the Windows scroll invariant.
// Complete-history navigation adds 0.3 KiB gzip (0.070%) to that production
// path while keeping its 1.68 KiB question rail lazy-loaded. Test diagnostics
// plus the navigation owner add 0.7 KiB gzip (0.164%) over the merged test gate.
// DingTalk channel status and locale wiring move the current-base production
// build from 427.2 to 427.7 KiB and test from 428.6 to 429.1 KiB. The unified
// state-aware geometry contract, session diagnostics counters, and guarded
// native-scroll probes add 2.4 KiB gzip to the initial path. The current
// main-v2 merge adds another 0.3 KiB of deterministic shared startup code.
// Keep the increase explicit and bounded instead of hiding it in a broad
// percentage ratchet.
// The retained-transcript surface adds a small, bounded navigation owner to
// the startup path (overlay state + stale-completion guard). Keep the increase
// explicit and narrow; the measured build is 431.1 KiB gzip.
// The web-search tool card now resolves the same display projection lazily so
// its filtered count matches the assistant Sources panel. The measured build
// is 431.509 KiB gzip; keep 0.1 KiB of explicit headroom for hash/toolchain
// drift instead of relying on a rounded equality.
const initialJSBudgetKiB = process.env.REASONIX_CHANNEL === "test" ? 431.6 : 431.6;
assertBudget("initial JavaScript gzip", initialJSGzip, initialJSBudgetKiB * 1024);
assertBudget("largest initial JavaScript chunk gzip", largestInitialJS, 280 * 1024);
// Render-blocking CSS is intentionally absent: styles.css loads deferred via
// ?url, and feature styles (heartbeat) live in lazy chunks loaded on demand.
// An empty initial CSS list is the desired state, not a build error.
if (initialCSS.length > 0) {
  assertBudget("render-blocking CSS gzip", initialCSSGzip, 4 * 1024);
} else {
  process.stdout.write("  PASS  render-blocking CSS: none (all styles deferred)\n");
}
// Extension surfaces, Task Monitor, and compact decision receipts share the
// application stylesheet loaded before React mounts. Keep their combined
// allowance bounded even though the file is no longer render-blocking.
// Navigation overlay styles add a bounded 0.1 KiB to the deferred shell.
// The cleaned source panel adds 0.1 KiB gzip to the deferred shell on top of
// the retained-transcript navigation allowance; keep the ratchet explicit.
assertBudget("deferred app-shell CSS gzip", appShellCSSGzip, 114.3 * 1024);
if (localeChunks.length !== 2) {
  throw new Error(`expected 2 on-demand Chinese locale chunks, found ${localeChunks.length}`);
}
for (const path of localeChunks) {
  const name = basename(path);
  // Task Monitor, billing, indexed history, Task Center, Extension UI, and
  // runtime controls plus execution-setting receipts add localized copy. The
  // write-access approval card adds four scoped actions and a home-risk
  // warning (~0.15 KiB gzip, +0.27% over the old 54.75 gate). Context
  // compaction settings add 40 bytes gzip of policy guidance to simplified
  // Chinese, while scheduled billing adds compact rate-band labels/tooltips.
  // The three StepFun presets add localized names/descriptions (~0.1 KiB
  // gzip); the two pay-as-you-go presets add the same again. The delivery
  // floor segmented control adds two labels plus one explanatory tooltip,
  // measured at 23 B gzip for zh and 8 B for zh-TW. Completion receipts add
  // six short status labels in each locale, requiring another 0.2 KiB per
  // language. DingTalk setup and mention guidance add at most 0.2 KiB more
  // (0.36%); retain the complete security and group-chat copy instead of
  // abbreviating user-facing instructions to fit the old locale ratchet.
  // Recovery-copy and catalog-only sidebar labels can move the simplified
  // Chinese chunk across the rounded 55.9 KiB boundary on CI's Node/zlib;
  // retain a narrow 0.1 KiB headroom rather than making gzip output a
  // platform-dependent gate. The OpenCode one-key setup adds product-level
  // connection, fallback, and legacy-state copy while removing protocol
  // choices from the primary UI; keep that complete guidance with a bounded
  // 0.4–0.5 KiB locale-only ratchet.
  const budget = name.startsWith("zh-TW-") ? 57.2 * 1024 : 56.5 * 1024;
  assertBudget(`${name} gzip`, gzipBytes(path), budget);
}

const rawInitialBytes = [...initialJS, ...initialCSS, ...appShellCSS]
  .reduce((total, path) => total + statSync(path).size, 0);
// The maintained Virtuoso engine adds 49.1 KiB raw (2.2%) over the previous
// 2268.7 KiB gate. Navigation remains inside the 2341 KiB production ceiling;
// its combined diagnostic wiring adds 2.2 KiB (0.094%) to the test channel.
// DingTalk startup wiring moves current-base production from 2341.0 to 2343.6
// KiB and test from 2346.2 to 2348.8 KiB; the pinned heading adds 0.5 KiB raw
// (0.021%). The workspace panel rework (change-row hover/revert, status badges,
// More menu, completion summary) makes the latest-base merge 2353.1 KiB in
// production and 2358.3 KiB in test: about 9.0 KiB (0.38%) over main-v2's
// channel gates. Retain that attributable UI capacity with 0.1 KiB of build-SHA
// headroom without widening the gzip or largest-chunk exceptions.
const rawInitialBudgetKiB = process.env.REASONIX_CHANNEL === "test" ? 2_358.4 : 2_353.2;
assertBudget("initial raw JavaScript and CSS", rawInitialBytes, rawInitialBudgetKiB * 1024);
assertBudget("largest initial JavaScript chunk raw", largestInitialJSRaw, 1_000 * 1024);
