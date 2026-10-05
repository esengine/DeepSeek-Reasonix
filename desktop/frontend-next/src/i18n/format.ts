import { current } from "./index";

// Numbers, in the reader's language. Before this, five files each carried their
// own k/M rule with different thresholds and decimal places, four spelled a
// duration to four different precisions, and money was a symbol glued to
// toFixed(2) — which put no thousands separator anywhere in the English window.
//
// What is NOT localised here: the technical suffixes. k, M, KB, MB and s read
// the same in both languages, while Intl's compact notation would render 12345
// as 1.2万 — a quantity feeling, not the scale a token count is measured on.
// Intl formats the number; the unit stays what the domain calls it.

const tag = () => (current() === "zh" ? "zh-CN" : "en-US");

// Constructing a formatter costs far more than using one, and these run per
// frame while a turn streams. The language is fixed for the session, so a
// cached formatter can never go stale.
const cache = new Map<string, Intl.NumberFormat>();

function nf(opts: Intl.NumberFormatOptions): Intl.NumberFormat {
  const key = current() + JSON.stringify(opts);
  let f = cache.get(key);
  if (!f) cache.set(key, (f = new Intl.NumberFormat(tag(), opts)));
  return f;
}

/** count is a plain whole number, grouped: 1234567 → "1,234,567". */
export const count = (n: number) => nf({ maximumFractionDigits: 0 }).format(n);

/** decimals is a number kept to exactly n places. */
export const decimals = (v: number, digits: number) =>
  nf({ minimumFractionDigits: digits, maximumFractionDigits: digits }).format(v);

/** tokens abbreviates a count of tokens or characters. Under ten thousand it
 *  keeps one decimal and above it drops to none, so the field's width stays put
 *  as a turn grows. */
export function tokens(n: number): string {
  if (n < 1000) return count(n);
  if (n < 1_000_000) return `${nf({ maximumFractionDigits: n < 10_000 ? 1 : 0 }).format(n / 1000)}k`;
  return `${decimals(n / 1_000_000, 1)}M`;
}

/** seconds spells a millisecond duration. One place by default — the callers
 *  that used to pick their own each picked a different one. */
export const seconds = (ms: number, digits = 1) => `${decimals(ms / 1000, digits)}s`;

const KB = 1 << 10;
const MB = 1 << 20;
const GB = 2 ** 30;
const TB = 2 ** 40;

/** bytes spells a size. Binary steps, because that is what the kernel counts.
 *  It runs to TB because storage roots and the volumes under them are measured
 *  there: capping at MB is how a 9 GB total came out as "9318.4 MB". */
export function bytes(n: number): string {
  if (n >= TB) return `${decimals(n / TB, 1)} TB`;
  if (n >= GB) return `${decimals(n / GB, 1)} GB`;
  if (n >= MB) return `${decimals(n / MB, 1)} MB`;
  if (n >= KB) return `${nf({ maximumFractionDigits: 0 }).format(n / KB)} KB`;
  return `${count(n)} B`;
}

/** pct takes a ratio, not a percentage: 0.5 → "50%". Intl owns the sign's
 *  placement, which is not the same in every language. */
export const pct = (ratio: number, digits = 0) =>
  nf({ style: "percent", minimumFractionDigits: digits, maximumFractionDigits: digits }).format(ratio);

// An amount smaller than half the last place a fixed precision can show rounds
// to the same string a true zero prints. A turn that cost $0.0029 then reads as
// one that cost nothing, which is the one thing a cost figure must not say.
// Ordinary amounts keep ordinary currency precision; only the ones that would
// vanish are given the significant digits it takes to stay non-zero.
const vanishes = (amount: number, digits: number) =>
  amount !== 0 && Math.abs(amount) < 0.5 * 10 ** -digits;

const scale = (amount: number, digits: number): Intl.NumberFormatOptions =>
  vanishes(amount, digits)
    ? { maximumSignificantDigits: 2 }
    : { minimumFractionDigits: digits, maximumFractionDigits: digits };

/** money formats an amount in its own currency. The ISO code is what the kernel
 *  sends and what Intl needs: it is the difference between ¥ and CN¥ in an
 *  English window, and a symbol glued to the front cannot make that distinction.
 *  An unknown code falls back to the code itself rather than dropping it. */
export function money(amount: number, code: string, digits = 2): string {
  const iso = code.trim().toUpperCase();
  if (/^[A-Z]{3}$/.test(iso)) {
    try {
      return nf({ style: "currency", currency: iso, ...scale(amount, digits) }).format(amount);
    } catch {
      // An ISO-shaped code this runtime does not know.
    }
  }
  return `${code}${nf(scale(amount, digits)).format(amount)}`;
}

// Plural categories are the language's, not a one-vs-many guess: English needs
// two and Chinese one, but Russian needs four and Arabic six. Asking Intl now
// means a catalogue that grows plural forms later does not also need this
// rewritten.
const plurals = new Map<string, Intl.PluralRules>();

/** category returns the CLDR plural category for n in the current language. */
export function category(n: number): Intl.LDMLPluralRule {
  const key = current();
  let r = plurals.get(key);
  if (!r) plurals.set(key, (r = new Intl.PluralRules(tag())));
  return r.select(n);
}
