/**
 * Shared formatting utilities for token counts and numeric displays.
 *
 * Centralises the previously duplicated fmtTokens / fmtFullTokens helpers
 * scattered across ContextPanel and Composer so every surface renders
 * identical compact token strings (e.g. "1.5K", "2.0M").
 */

/** Options controlling how {@link formatTokens} renders a count. */
export interface TokenFormatOptions {
  /**
   * When `true` (the default) large values are abbreviated with K / M suffixes.
   * When `false` the raw locale-formatted number is returned instead.
   */
  compact?: boolean;

  /** Maximum fractional digits kept in the abbreviated form (default `1`). */
  decimals?: number;

  /**
   * Minimum value before the K-suffix kicks in (default `1000`).
   * Useful for contexts that want to show plain numbers up to a higher bound.
   */
  threshold?: number;
}

/** Strip trailing zeros after the decimal point: "1.50" → "1.5", "1.00" → "1". */
function stripTrailingZeros(s: string): string {
  return s.replace(/(\.\d*?)0+$/, "$1").replace(/\.$/, "");
}

/**
 * Format a token count for human-readable display.
 *
 * Behaviour:
 *  - Returns `"-"` for missing, zero, or negative inputs.
 *  - In compact mode (default): values >= 1 000 000 use the `M` suffix,
 *    values >= `threshold` (default 1 000) use the `K` suffix, and smaller
 *    values are rendered as-is.
 *  - Trailing `.0` is stripped so `1000` becomes `"1K"` rather than `"1.0K"`.
 *  - In non-compact mode `toLocaleString()` is used for locale-aware grouping.
 *
 * @example
 *  formatTokens(1500)        // "1.5K"
 *  formatTokens(1000)        // "1K"
 *  formatTokens(142000)      // "142K"
 *  formatTokens(1_500_000)   // "1.5M"
 *  formatTokens(999)         // "999"
 *  formatTokens(undefined)   // "-"
 */
export function formatTokens(tokens: number | undefined, options?: TokenFormatOptions): string {
  if (typeof tokens !== "number" || tokens <= 0) return "-";

  const { compact = true, decimals = 1, threshold = 1000 } = options ?? {};

  if (!compact) {
    return tokens.toLocaleString();
  }

  if (tokens >= 1_000_000) {
    return `${stripTrailingZeros((tokens / 1_000_000).toFixed(decimals))}M`;
  }
  if (tokens >= threshold) {
    return `${stripTrailingZeros((tokens / 1000).toFixed(decimals))}K`;
  }

  return String(tokens);
}

/**
 * Convenience wrapper for optional token counts (session / turn totals).
 *
 * Delegates to {@link formatTokens} and returns `"-"` when the value is
 * absent, zero, or negative — matching the old `fmtOptionalTokens` helper
 * it replaces.
 */
export function formatOptionalTokens(tokens?: number | null, options?: TokenFormatOptions): string {
  if (typeof tokens !== "number" || tokens <= 0) return "-";
  return formatTokens(tokens, options);
}
