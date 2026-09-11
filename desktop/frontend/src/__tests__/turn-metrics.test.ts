import assert from "node:assert/strict";
import {
  formatElapsedMs, outputQuarters, tokensFromQuarters, turnMetrics, unbilledOutputTokens,
} from "../lib/turnMetrics";

const eq = (actual: unknown, expected: unknown, label: string) =>
  assert.equal(actual, expected, label);
const ok = (value: unknown, label: string) => assert.ok(value, label);

// The flat `chars / 4` this module replaces. Every ASCII fixture in the suite
// was written against this expression, so any divergence is a regression.
const legacyEstimate = (chars: number) => Math.round(chars / 4);

// --- ASCII identity: the load-bearing guarantee -----------------------------
for (const n of [0, 1, 3, 4, 5, 7, 8, 15, 16, 17, 40, 41, 99, 100, 101, 1_000, 4_003]) {
  const s = "x".repeat(n);
  eq(outputQuarters(s), n, `ASCII weights one quarter per char (n=${n})`);
  eq(tokensFromQuarters(outputQuarters(s)), legacyEstimate(n), `ASCII estimate matches chars/4 (n=${n})`);
}
eq(tokensFromQuarters(0), 0, "empty output estimates zero");

// --- CJK density ------------------------------------------------------------
// Han is three UTF-8 bytes, so it is worth three times the old flat density.
eq(outputQuarters("中"), 3, "a Han character carries three quarters");
eq(tokensFromQuarters(outputQuarters("中".repeat(16))), 12, "16 Han chars estimate 12 tokens");
eq(legacyEstimate(16), 4, "the legacy flat estimate would have said 4");
ok(tokensFromQuarters(outputQuarters("中".repeat(16))) > legacyEstimate(16) * 2,
  "CJK output is no longer underestimated threefold");
eq(outputQuarters("é"), 2, "a two-byte rune carries two quarters");
eq(outputQuarters("😀"), 4, "an astral pair carries four quarters and consumes both units");
eq(outputQuarters("中".repeat(4), 2), 6, "the start index skips leading characters");

// Mixed scripts: density must sit between the ASCII floor and the Han ceiling.
const mixed = "hi 中文 there";
const mixedQuarters = outputQuarters(mixed);
ok(mixedQuarters > mixed.length, "mixed text outweighs its character count");
ok(mixedQuarters < mixed.length * 3, "mixed text stays below the pure-Han ceiling");
eq(outputQuarters("中文abc"), outputQuarters("abc中文"), "weight is order-independent");

// --- Unbilled window --------------------------------------------------------
const buf = (text: string, reasoning = "") => ({ text, reasoning });
eq(unbilledOutputTokens(undefined, 0, 0), 0, "no buffers estimates nothing");
eq(unbilledOutputTokens(buf("x".repeat(40)), 0, 0), 10, "whole ASCII buffer matches chars/4");
eq(unbilledOutputTokens(buf("x".repeat(40)), 0, 0), legacyEstimate(40), "ASCII parity at the buffer level");
eq(unbilledOutputTokens(buf("中".repeat(40)), 0, 0), 30, "whole CJK buffer is weighted");
eq(unbilledOutputTokens(buf("x".repeat(40)), 16, 0), 6, "billed prefix is excluded from the estimate");
eq(unbilledOutputTokens(buf("x".repeat(40)), 16, 0), legacyEstimate(24), "partial-bill ASCII parity");
eq(unbilledOutputTokens(buf("", ""), 0, 40), 10, "argument characters are charged as ASCII");
eq(unbilledOutputTokens(buf("x".repeat(8)), 0, 8), 4, "buffer and arguments sum before rounding");
eq(unbilledOutputTokens(buf("x".repeat(4)), 99, 0), 0, "an over-billed buffer floors at zero");
// Tail slice: all of `text` is billed before any of `reasoning`.
eq(unbilledOutputTokens(buf("x".repeat(20), "中".repeat(4)), 8, 0), 4,
  "a billed prefix is consumed from the text buffer first");
eq(unbilledOutputTokens(buf("x".repeat(20), "中".repeat(4)), 4, 0), 5,
  "a window ending exactly at the text boundary stays pure ASCII weight");
eq(unbilledOutputTokens(buf("x".repeat(20), "中".repeat(8)), 2, 0), 10,
  "a window reaching into the reasoning tail picks up Han weight");
ok(unbilledOutputTokens(buf("x".repeat(20), "中".repeat(8)), 2, 0) > legacyEstimate(26),
  "the reasoning tail is no longer priced as ASCII");

// --- The consolidated derivation matches the inline expression it replaces ---
const legacyRunMetrics = (i: Parameters<typeof turnMetrics>[0]) => {
  if (!i.turnStartAt || (!i.running && !i.turnDoneAt)) return null;
  const metricsNow = i.turnDoneAt || i.now;
  const elapsedMs = Math.max(0, metricsNow - i.turnStartAt
    - (i.turnDoneAt ? i.lastTurnWaitAccumMs ?? i.waitAccumMs : i.waitAccumMs));
  const liveChars = (i.live?.text.length ?? 0) + (i.live?.reasoning.length ?? 0);
  const inFlightChars = Math.max(0, liveChars - (i.turnOutputCharsAtUsage ?? 0)) + (i.turnArgChars ?? 0);
  const estimatedChars = !i.turnDoneAt ? legacyEstimate(inFlightChars)
    : Math.max(0, (i.lastTurnOutputTokens ?? i.turnOutputTokens ?? 0) - (i.turnOutputTokens ?? 0));
  const outTok = (i.turnOutputTokens ?? 0) + estimatedChars;
  const modelActiveAt = i.liveModelActiveAt ?? i.turnModelActiveAt;
  const modelElapsedMs = Math.max(0, i.turnModelActiveMs
    + (modelActiveAt && modelActiveAt > 0 ? Math.max(0, metricsNow - modelActiveAt) : 0));
  const tps = outTok > 0 && modelElapsedMs >= 500 ? Math.round(outTok / (modelElapsedMs / 1000)) : null;
  return { tokens: (i.turnTokens ?? 0) + estimatedChars, outTok, elapsedMs, tps };
};
const base = {
  now: 60_000, turnStartAt: 1_000, turnDoneAt: undefined, running: true, waitAccumMs: 0,
  turnTokens: 8, turnOutputTokens: 10, turnModelActiveMs: 2_000,
  turnOutputCharsAtUsage: 0, turnArgChars: 0, live: buf("x".repeat(40)),
};
for (const fixture of [
  base,
  { ...base, turnDoneAt: 20_000, lastTurnOutputTokens: 24, lastTurnWaitAccumMs: 5_000 },
  { ...base, waitAccumMs: 3_000, turnModelActiveAt: 55_000, liveModelActiveAt: 58_000 },
  { ...base, turnModelActiveMs: 400, live: undefined },
  { ...base, turnOutputTokens: 0, turnTokens: 0, live: buf("", "") },
  { ...base, live: buf("x".repeat(40), "y".repeat(8)), turnArgChars: 12 },
]) {
  const next = turnMetrics(fixture);
  const legacy = legacyRunMetrics(fixture);
  ok(next && legacy, "both derivations produce a reading for the ASCII fixture");
  eq(next!.tokens, legacy!.tokens, "token reading matches the inline expression");
  eq(next!.outputTokens, legacy!.outTok, "output reading matches the inline expression");
  eq(next!.elapsedMs, legacy!.elapsedMs, "elapsed reading matches the inline expression");
  eq(next!.tps, legacy!.tps, "throughput reading matches the inline expression");
}
eq(turnMetrics({ ...base, turnStartAt: 0 }), null, "a turn without a start anchor has no reading");
eq(turnMetrics({ ...base, running: false, turnDoneAt: undefined }), null,
  "neither running nor completed yields no reading");
eq(turnMetrics({ ...base, running: false, turnDoneAt: 20_000 })!.tps, 5,
  "a settled turn divides its billed output by the model-active window");
eq(turnMetrics(base)!.tps, 10, "a streaming turn adds the in-flight estimate to the numerator");
eq(turnMetrics({ ...base, turnModelActiveMs: 400 })!.tps, null, "sub-500ms windows report no throughput");
eq(turnMetrics({ ...base, running: false, turnDoneAt: 20_000, lastTurnOutputEstimated: true })!.estimated,
  true, "a settled estimate is flagged");
eq(turnMetrics(base)!.estimated, true, "a streaming estimate is flagged");
eq(turnMetrics({ ...base, turnOutputEstimated: false, live: buf("") })!.estimated, false,
  "a stream with nothing estimated is not flagged");

// --- Elapsed label ----------------------------------------------------------
eq(formatElapsedMs(0), "0s", "zero seconds");
eq(formatElapsedMs(4_999), "4s", "sub-minute truncates");
eq(formatElapsedMs(20_000), "20s", "seconds");
eq(formatElapsedMs(60_000), "1m 0s", "minute boundary");
eq(formatElapsedMs(83_421), "1m 23s", "minutes and seconds");

console.log("turn metrics: all assertions passed");
