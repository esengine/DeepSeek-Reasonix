import { describe, it, expect } from "vitest";
import { formatTokens, formatOptionalTokens } from "../lib/format";

describe("formatTokens", () => {
  it("returns '-' for undefined, zero, and negative values", () => {
    expect(formatTokens(undefined)).toBe("-");
    expect(formatTokens(0)).toBe("-");
    expect(formatTokens(-100)).toBe("-");
  });

  it("formats values below threshold as plain numbers", () => {
    expect(formatTokens(1)).toBe("1");
    expect(formatTokens(500)).toBe("500");
    expect(formatTokens(999)).toBe("999");
  });

  it("abbreviates thousands with K suffix", () => {
    expect(formatTokens(1000)).toBe("1K");
    expect(formatTokens(1500)).toBe("1.5K");
    expect(formatTokens(142000)).toBe("142K");
    expect(formatTokens(999999)).toBe("1000K");
  });

  it("abbreviates millions with M suffix", () => {
    expect(formatTokens(1_000_000)).toBe("1M");
    expect(formatTokens(1_500_000)).toBe("1.5M");
    expect(formatTokens(25_000_000)).toBe("25M");
  });

  it("strips trailing .0 for exact multiples", () => {
    expect(formatTokens(1000)).toBe("1K");
    expect(formatTokens(2000)).toBe("2K");
    expect(formatTokens(1_000_000)).toBe("1M");
  });

  it("keeps one decimal place when needed", () => {
    expect(formatTokens(1500)).toBe("1.5K");
    expect(formatTokens(234000)).toBe("234K");
    expect(formatTokens(1_230_000)).toBe("1.2M");
  });

  it("respects custom decimals option", () => {
    expect(formatTokens(1500, { decimals: 2 })).toBe("1.5K");
    expect(formatTokens(1555, { decimals: 2 })).toBe("1.55K");
    expect(formatTokens(1_555_000, { decimals: 2 })).toBe("1.55M");
  });

  it("respects custom threshold option", () => {
    expect(formatTokens(500, { threshold: 100 })).toBe("0.5K");
    expect(formatTokens(99, { threshold: 100 })).toBe("99");
    expect(formatTokens(100, { threshold: 100 })).toBe("0.1K");
  });

  it("returns locale-formatted string when compact is false", () => {
    const result = formatTokens(142000, { compact: false });
    // toLocaleString output varies by locale, but should contain digits
    expect(result).toMatch(/142/);
    expect(result).not.toMatch(/[KM]/);
  });
});

describe("formatOptionalTokens", () => {
  it("returns '-' for missing, zero, and negative values", () => {
    expect(formatOptionalTokens(undefined)).toBe("-");
    expect(formatOptionalTokens(null)).toBe("-");
    expect(formatOptionalTokens(0)).toBe("-");
    expect(formatOptionalTokens(-50)).toBe("-");
  });

  it("formats positive values identically to formatTokens", () => {
    expect(formatOptionalTokens(1500)).toBe("1.5K");
    expect(formatOptionalTokens(1_000_000)).toBe("1M");
    expect(formatOptionalTokens(500)).toBe("500");
  });
});
