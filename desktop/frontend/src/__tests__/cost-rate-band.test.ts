import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { mergeRateBand, normalizeRateBand, rateBandLabel } from "../lib/costRateBand";

describe("cost rate band aggregation", () => {
  it("keeps a uniform band and marks crossings mixed", () => {
    assert.equal(mergeRateBand(undefined, "peak"), "peak");
    assert.equal(mergeRateBand("peak", "peak"), "peak");
    assert.equal(mergeRateBand("peak", "off_peak"), "mixed");
    assert.equal(mergeRateBand("mixed", "peak"), "mixed");
  });

  it("does not declare a band after an unknown or legacy quote", () => {
    assert.equal(mergeRateBand(undefined, undefined), "unknown");
    assert.equal(mergeRateBand("peak", undefined), "unknown");
    assert.equal(mergeRateBand("unknown", "off_peak"), "unknown");
    assert.equal(normalizeRateBand("future_band"), undefined);
  });

  it("uses a compact, unambiguous label for the mixed-rate badge", () => {
    const t = (key: string) => key === "billing.rateBand.mixed" ? "峰谷混合" : key;
    assert.equal(rateBandLabel("mixed", t), "峰谷混合");
    assert.equal(rateBandLabel("future_band", t), undefined);
  });
});
