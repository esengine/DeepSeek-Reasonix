import { describe, expect, it } from "vitest";
import {
  COMPACTION_SUMMARY_MARKER,
  isCompactionSummary,
  stripCompactionMarker,
} from "../src/compaction.js";

describe("compaction helpers", () => {
  it("detects only when the marker is at the start", () => {
    expect(isCompactionSummary(`${COMPACTION_SUMMARY_MARKER}body`)).toBe(true);
    expect(isCompactionSummary("ok body")).toBe(false);
    expect(isCompactionSummary(`prefix ${COMPACTION_SUMMARY_MARKER}body`)).toBe(false);
    expect(isCompactionSummary("")).toBe(false);
    expect(isCompactionSummary(null)).toBe(false);
    expect(isCompactionSummary(undefined)).toBe(false);
  });

  it("strips the marker only when it is at the start", () => {
    expect(stripCompactionMarker(`${COMPACTION_SUMMARY_MARKER}body`)).toBe("body");
    expect(stripCompactionMarker("untouched")).toBe("untouched");
  });
});
