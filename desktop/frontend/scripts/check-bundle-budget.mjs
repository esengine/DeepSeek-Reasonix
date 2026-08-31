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
// Remote onboarding [0.5/3] adds project-group and credential-chain wiring on
// top of the lazy wizard. Exact-turn routing, the extracted event-gap
// projector, checkpoint resets, and the navigation surface transaction bring
// the current main-v2 path to 437.36 KiB gzip.
// The full remote-session surface adds the lazy transcript bridge and tab
// lifecycle on top of [0.5/3]. Keep the measured stack's narrow ratchet.
// Remote approval hardening adds authoritative composer-profile hydration,
// scoped rewind dispatch, and attachment/inbox fences to the always-mounted
// remote hook. The measured production path is 438.38 KiB gzip after keeping
// the integration modules below repolint's ownership ceilings; retain 0.12 KiB
// toolchain headroom with a bounded 1.4 KiB ratchet.
// Remote status isolation keeps the always-mounted status bar on the active
// remote transcript and routes job cancellation to that host. Parsers and
// retry policy remain lazy; the measured selector adds under 0.1 KiB gzip.
// Remote runtime parity adds scoped approvals, status-only reconciliation,
// session quality-floor routing, dropped-frame reconciliation, and remote
// runtime-command dispatch. The measured initial path is 439.60 KiB;
// retain 0.10 KiB of bounded toolchain headroom.
// Closing the remaining review gaps adds generation-fenced hydration plus
// remote-only tool payload, Todo, and terminal isolation. The measured path is
// 439.74 KiB; retain 0.06 KiB of headroom with a 0.1 KiB ratchet.
// The final remote-runtime parity pass adds remote run-strip telemetry,
// explicit session verbs, and specialized plan decisions. The measured path
// is 440.02 KiB. The current main-v2 turn-event, finish-protocol, and session
// repair runtime then moves the combined path to 445.097 KiB; retain 0.103 KiB
// of bounded build/toolchain headroom.
// Atomic remote profile changes, exact approval draining, and generation-safe
// history handoff bring the measured path to 445.228 KiB. Retain 0.072 KiB of
// headroom with the smallest existing decimal ratchet.
// Direct pending-prompt recovery and authoritative remote Goal state bring the
// measured path to 445.473 KiB. Retain 0.027 KiB of bounded headroom.
// Restored remote shells now activate their backend session immediately and
// keep disconnected state out of the mounted surface. The merged production
// path measures 445.614 KiB; retain 0.086 KiB of bounded build/toolchain
// headroom with the smallest existing decimal ratchet.
// Runtime-aware Todo presentation plus exact-tab continuation adds 0.3 KiB gzip
// to the always-mounted footer path. Keep the state/routing guard with a narrow
// ratchet rather than showing idle restored work as actively running. The
// combined path measures 445.9 KiB; retain 0.1 KiB of toolchain headroom.
// Transcript surface ownership and the token-fenced unloaded-question commit
// move the exact main-v2 baseline from 445.865 to 447.587 KiB gzip (+0.39%).
// The final 0.266 KiB retains jump ownership through paint-ready instead of
// allowing a native scrollend to release it. Keep only 0.213 KiB headroom;
// native validation hosts and test fixtures stay outside the production graph.
// Cross-platform shell inventory, current-session vs after-reload rows,
// manual repair guidance, and exact download-host allowlisting move the merged
// path from 448.692 to 449.758 KiB (+1.066 KiB). Retain 0.142 KiB of bounded
// build/toolchain headroom.
// The reader transaction contract (geometry revisions, generation-fenced
// writer requests, gesture travel proof, stabilized-shrink extent acceptance,
// and the blank-rebound prepaint lane) adds a measured 3.978 KiB gzip on the
// merged main-v2 baseline. MCP elicitation and the inline Apps lifecycle remain
// on that startup graph; the combined path measures 455.0 KiB. Retain 0.2 KiB
// of bounded build/toolchain headroom.
// Generic elicitation validation adds field-specific localized accessibility
// copy to the English startup dictionary. The interaction code and CSS remain
// lazy; the measured path is 455.437 KiB. Retain 0.163 KiB of headroom.
// Stream-failure visibility (#9560) adds the last-discard reason and one
// terminal-notice dedupe flag, while provider no_proxy copy now states the
// custom-proxy precedence. The merged path measures 455.9 KiB; retain 0.1 KiB
// of bounded build/toolchain headroom.
// Exhausted tail repair now releases ownership so jump-bottom remains usable
// after a stranded native WebView extent. The WebView2 reachable-tail clamp
// then absorbs a second post-quiet extent without an unbounded write loop.
// The combined path measures 456.316 KiB; retain 0.084 KiB with the smallest
// one-decimal ratchet.
// The generation-bound history-prepend lease adds stable-key reader anchoring,
// full mounted coverage, and one final arbiter-owned correction. The measured
// path is 457.406 KiB after extracting the lease owner to satisfy repolint.
// Latest-base transcript settle ownership measures 457.518 KiB with this UX;
// isolated conversation forks bring the combined tree to 458.158 KiB.
// Worktree merge-back adds the review modal, diff statistics, and conflict checks.
// The combined path measures 459.0 KiB; retain bounded toolchain headroom.
const initialJSBudgetKiB = process.env.REASONIX_CHANNEL === "test" ? 462.0 : 462.0;
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
// The navigation mask's stable composer footprint and remote tab/surface
// states bring the merged shell to roughly 115.7 KiB gzip.
assertBudget("deferred app-shell CSS gzip", appShellCSSGzip, 116.0 * 1024);
if (localeChunks.length !== 2) {
  throw new Error(`expected 2 on-demand Chinese locale chunks, found ${localeChunks.length}`);
}
for (const path of localeChunks) {
  const name = basename(path);
  // Worktree merge and fork guidance adds localized copy across dialects.
  const budget = name.startsWith("zh-TW-") ? 63.0 * 1024 : 62.0 * 1024;
  assertBudget(`${name} gzip`, gzipBytes(path), budget);
}

const rawInitialBytes = [...initialJS, ...initialCSS, ...appShellCSS]
  .reduce((total, path) => total + statSync(path).size, 0);
// Latest-base transcript settle ownership brings the measured path to
// 2452.773 KiB; isolated conversation forks bring the combined tree to
// 2454.719 KiB on the release toolchain.
// Worktree merge-back adds review modal, diff checks, and conflict detection.
// The combined payload measures 2457.3 KiB; retain bounded toolchain headroom.
const rawInitialBudgetKiB = process.env.REASONIX_CHANNEL === "test" ? 2_465.0 : 2_465.0;
assertBudget("initial raw JavaScript and CSS", rawInitialBytes, rawInitialBudgetKiB * 1024);
assertBudget("largest initial JavaScript chunk raw", largestInitialJSRaw, 1_000 * 1024);

