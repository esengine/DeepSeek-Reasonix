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
// Transcript scroll integrity adds the single-writer gateway, native reader
// correction bridge, failure-atomic unloaded-question mask, and bounded tail
// handoff on top of that remote-session surface. The merged build measures
// 450.6 KiB. Persisting native-thumb bottom proof measures 450.8 KiB; retain
// 0.1 KiB of bounded headroom without widening the per-chunk ratchet. The
// native pre-paint closure retains the last painted row, rejects a blank
// virtual range, and samples thumb-bottom before React delivery; the measured
// path is 451.2 KiB. Retaining the last complete painted range closes the
// remaining WKWebView boundary-row race at 451.6 KiB. The same-paint native
// acknowledgement bridge, transform fence, and adaptive reader buffer measure
// 452.1 KiB. Native-delivery direction synchronization and its correction-
// acknowledgement fence close the controller-input WebView gap. Accepting a
// coalesced native delivery before observation and releasing a passed forward
// correction measure 452.473 KiB. The native blank hold, WKWebView's second
// compositor viewport, Footer extent observer, and physical-LAST sync measure
// 452.675 KiB. Native-thumb pointer travel closes the remaining coalesced
// away-and-back release gap at 452.853 KiB. Synchronously retaining every
// accepted native frame and fencing an unacknowledged correction measure
// 452.953 KiB. The bounded settle-resend guard stays within 0.1 KiB of that
// mark; the reading-anchor pin plus the gateway sync normalization measure
// 453.6 KiB. Viewport-preserving indexed offsets and coalesced extent/input
// classification retain 0.1 KiB of gzip headroom. The pre-paint native-range
// clamp, captured thumb-travel proof, and bounded LAST fallback measure
// 453.852 KiB; keep them with a 0.1 KiB decimal ratchet and roughly 0.048 KiB
// of remaining headroom.
// Confirming the native extent after a logical-LAST handoff, remounting an
// unmounted tail before paint, and re-arming tail convergence when the
// committed scroller replaces its hydration predecessor, plus the bounded
// post-range native confirmation, measure 454.3 KiB.
// Retain about 0.1 KiB of toolchain headroom without widening any per-chunk
// budget.
// Restored remote shells now activate their backend session immediately and
// keep disconnected state out of the mounted surface. The merged production
// path measures 445.614 KiB; retain 0.086 KiB of bounded build/toolchain
// headroom with the smallest existing decimal ratchet.
// Runtime-aware Todo presentation plus exact-tab continuation adds 0.3 KiB gzip
// to the always-mounted footer path. Keep the state/routing guard with a narrow
// ratchet rather than showing idle restored work as actively running. The
// main-v2-only path measures 445.9 KiB. The integrated transcript and remote
// changes measure 454.7 KiB. Synchronizing no-common reader ranges, rejecting
// non-adjacent painted ranges, and limiting pins to extent-backed slides
// measures 454.998 KiB; retain about 0.10 KiB of toolchain headroom.
// The native-thumb release proof and WebView2-only second compositor viewport
// measure 455.103 KiB; retain the same narrow 0.1 KiB toolchain allowance.
// Retaining the pre-swap painted range across a coalesced native-direction
// handoff and task-sampling a compositor-owned thumb measure 455.4 KiB. A
// one-frame bottom-hold commit and the bounded GTK compatibility-release wait
// measure 455.564 KiB; retain about 0.14 KiB of gzip/toolchain headroom.
const initialJSBudgetKiB = process.env.REASONIX_CHANNEL === "test" ? 455.7 : 455.7;
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
// states bring the merged shell to roughly 115.7 KiB gzip. The transcript
// completion overlay keeps the live Footer out of Virtuoso's measured flow.
assertBudget("deferred app-shell CSS gzip", appShellCSSGzip, 116.0 * 1024);
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
  // Remote-session actions and disconnected-shell guidance add bounded copy.
  const budget = name.startsWith("zh-TW-") ? 58.5 * 1024 : 57.8 * 1024;
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
// production and test channels both measure 2357.92 KiB after project-group
// wiring. Exact-turn routing, checkpoint resets, and failure-atomic navigation
// bring the current main-v2 path to 2379.22 KiB. The remote approval
// fences, extracted ownership modules, and remote status-bar isolation bring
// the measured initial payload to 2380.9 KiB; retain 0.1 KiB of bounded
// raw/toolchain headroom. Scoped remote approvals, status reconciliation, and
// runtime command dispatch bring the measured payload to 2382.9 KiB. The
// remaining review fences measure 2383.2 KiB; retain 0.1 KiB of headroom.
// Final remote-runtime parity measures 2384.4 KiB raw. The current main-v2
// runtime additions bring the combined path to 2404.364 KiB; retain 0.136 KiB
// of bounded headroom alongside the gzip ratchet above.
// Transcript navigation and scroll integrity add target commit fencing,
// native reader stabilization, and the privacy-safe unloaded-question replay.
// The merged build measures 2424.5 KiB. Correction-ack validation and a real
// wall-clock native-tail bound and release proof bring it to 2425.2 KiB;
// the final pre-paint anchor and passive native-bottom sampler measure 2426.4
// KiB. The complete painted-range guard, post-paint baseline fence, and
// extent-scoped wheel proof measure 2428.0 KiB. The same-paint native bridge
// its transform feedback fence, and adaptive reader buffer measure 2429.5 KiB.
// Native-delivery direction synchronization, coalesced-input classification,
// and the passed-forward-correction fence measure 2430.713 KiB raw. The
// native blank hold, platform-bounded reader window, Footer observer, and
// physical-LAST sync measure 2431.764 KiB; retain 0.236 KiB of headroom.
// Retaining the preceding painted range across duplicate observer promotions,
// sampling native-thumb movement on real scroll delivery, and carrying the
// reader correction through its bounded tail handoff measure 2432.297 KiB.
// Native-thumb pointer travel brings the measured path to 2432.661 KiB. The
// accepted-frame and pending-correction fences measure 2432.735 KiB; the
// bounded settle-resend guard measures 2433.1 KiB; the reading-anchor pin,
// its tail-proximity gate and the gateway normalization reach 2435.8 KiB.
// Indexed reader offsets plus opposite extent/input classification measure
// 2436.6 KiB; the merged selection compositor bridge reaches 2436.8 KiB.
// The native-range clamp, captured thumb proof, and bounded LAST fallback
// measure 2437.342 KiB. Post-LAST native confirmation, unmounted-tail remount,
// committed-scroller re-arm, and bounded post-range native confirmation bring
// the integrated surface to 2439.1 KiB. Retain about 0.1 KiB of raw/toolchain
// headroom without widening the per-chunk ceiling.
// runtime additions bring the combined path to 2404.364 KiB. The final merged
// restored-shell activation and disconnected-state revival path measures
// 2404.898 KiB; retain 0.102 KiB of bounded headroom alongside the gzip
// ratchet above.
// Runtime-aware Todo status and exact-tab continuation then add to the same
// initial path. The main-v2-only payload measures 2406.2 KiB. The integrated
// payload measures 2441.2 KiB. Retaining the last painted reader baseline
// across several same-offset range candidates brings that path to 2441.4 KiB.
// Synchronizing a fully replaced occupied range, rejecting non-adjacent
// painted candidates, and retaining user-owned stable-extent slides measures
// 2442.144 KiB. Latching native-thumb movement from real scroll delivery
// measures 2442.3 KiB; retain about 0.1 KiB of raw/toolchain headroom.
// The coalesced direction handoff and compositor-task thumb proof measure
// 2442.994 KiB; retain about 0.1 KiB of raw/toolchain headroom.
// Preserving the prior painted candidate and deferring mouse-pointer release
// to Chromium's compatibility mouseup with a missing-event fallback measure
// 2443.3 KiB. Preferring the newest repairable painted frame over stale range
// history measures 2443.477 KiB. Retaining the reader buffer through its final
// bottom-hold paint and waiting for GTK's compatibility mouseup measure
// 2443.907 KiB; retain about 0.19 KiB of raw/toolchain headroom.
const rawInitialBudgetKiB = process.env.REASONIX_CHANNEL === "test" ? 2_444.1 : 2_444.1;
assertBudget("initial raw JavaScript and CSS", rawInitialBytes, rawInitialBudgetKiB * 1024);
assertBudget("largest initial JavaScript chunk raw", largestInitialJSRaw, 1_000 * 1024);
