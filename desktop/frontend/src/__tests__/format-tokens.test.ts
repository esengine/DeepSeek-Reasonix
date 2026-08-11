import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { formatTokens, formatOptionalTokens } from "../lib/format";

describe("formatTokens", () => {
  it("returns '-' for undefined, zero, and negative values", () => {
    assert.equal(formatTokens(undefined), "-");
    assert.equal(formatTokens(0), "-");
    assert.equal(formatTokens(-100), "-");
  });

  it("formats values below threshold as plain numbers", () => {
    assert.equal(formatTokens(1), "1");
    assert.equal(formatTokens(500), "500");
    assert.equal(formatTokens(999), "999");
  });

  it("abbreviates thousands with K suffix", () => {
    assert.equal(formatTokens(1000), "1K");
    assert.equal(formatTokens(1500), "1.5K");
    assert.equal(formatTokens(142000), "142K");
    assert.equal(formatTokens(999999), "1000K");
  });

  it("abbreviates millions with M suffix", () => {
    assert.equal(formatTokens(1_000_000), "1M");
    assert.equal(formatTokens(1_500_000), "1.5M");
    assert.equal(formatTokens(25_000_000), "25M");
  });

  it("strips trailing .0 for exact multiples", () => {
    assert.equal(formatTokens(1000), "1K");
    assert.equal(formatTokens(2000), "2K");
    assert.equal(formatTokens(1_000_000), "1M");
  });

  it("keeps one decimal place when needed", () => {
    assert.equal(formatTokens(1500), "1.5K");
    assert.equal(formatTokens(234000), "234K");
    assert.equal(formatTokens(1_230_000), "1.2M");
  });

  it("respects custom decimals option", () => {
    assert.equal(formatTokens(1500, { decimals: 2 }), "1.5K");
    assert.equal(formatTokens(1555, { decimals: 2 }), "1.55K");
    assert.equal(formatTokens(1_555_000, { decimals: 2 }), "1.55M");
  });

  it("respects custom threshold option", () => {
    assert.equal(formatTokens(500, { threshold: 100 }), "0.5K");
    assert.equal(formatTokens(99, { threshold: 100 }), "99");
    assert.equal(formatTokens(100, { threshold: 100 }), "0.1K");
  });

  it("returns locale-formatted string when compact is false", () => {
    const result = formatTokens(142000, { compact: false });
    // toLocaleString output varies by locale, but should contain digits
    assert.match(result, /142/);
    assert.doesNotMatch(result, /[KM]/);
  });
});

describe("formatOptionalTokens", () => {
  it("returns '-' for missing, zero, and negative values", () => {
    assert.equal(formatOptionalTokens(undefined), "-");
    assert.equal(formatOptionalTokens(null), "-");
    assert.equal(formatOptionalTokens(0), "-");
    assert.equal(formatOptionalTokens(-50), "-");
  });

  it("formats positive values identically to formatTokens", () => {
    assert.equal(formatOptionalTokens(1500), "1.5K");
    assert.equal(formatOptionalTokens(1_000_000), "1M");
    assert.equal(formatOptionalTokens(500), "500");
  });
});
