// Mirrors internal/base/tokencount.Text: Latin near four bytes per token, CJK near
// one token per character. Used to size streamed deltas, which arrive as text
// and carry no count of their own — usage only lands at the end of a round.
export function estimateTokens(s: string): number {
  let narrow = 0;
  let wide = 0;
  for (const ch of s) (ch.codePointAt(0)! < 128 ? narrow++ : wide++);
  return Math.ceil(narrow / 4) + wide;
}

export interface Sample {
  t: number;
  n: number;
}

// Long enough to smooth a bursty stream, short enough that the number falls to
// zero while a tool runs instead of implying the model is still writing.
export const WINDOW_MS = 4000;

export function sample(win: Sample[], n: number, now: number): Sample[] {
  return [...win, { t: now, n }].filter((s) => now - s.t <= WINDOW_MS);
}

// Rate over what actually arrived, not totals over turn time: session-cumulative
// output divided by this turn's clock reads as thousands of tokens per second on
// the first second of every turn after the first.
export function tokensPerSecond(win: Sample[], now: number): number {
  const live = win.filter((s) => now - s.t <= WINDOW_MS);
  if (live.length < 2) return 0;
  const span = (now - live[0].t) / 1000;
  if (span <= 0) return 0;
  return live.reduce((n, s) => n + s.n, 0) / span;
}
