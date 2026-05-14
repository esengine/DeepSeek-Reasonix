import { describe, expect, it } from "vitest";
import {
  type LiveTokenCalibration,
  estimateLiveTokenCount,
} from "../src/cli/ui/cards/StreamingCard.js";

function counter() {
  const calls: string[] = [];
  return {
    calls,
    count: (value: string) => {
      calls.push(value);
      return value.length * 2;
    },
  };
}

describe("estimateLiveTokenCount", () => {
  it("calibrates exactly for the first non-empty streaming text", () => {
    const tokenizer = counter();
    const result = estimateLiveTokenCount("hello", "card-1", null, tokenizer.count);

    expect(result.exact).toBe(true);
    expect(result.tokens).toBe(10);
    expect(result.calibration).toEqual({ cardId: "card-1", chars: 5, tokens: 10 });
    expect(tokenizer.calls).toEqual(["hello"]);
  });

  it("estimates small live growth without re-tokenizing the whole text", () => {
    const tokenizer = counter();
    const first = estimateLiveTokenCount("hello", "card-1", null, tokenizer.count);
    const grown = estimateLiveTokenCount(
      `hello${"x".repeat(120)}`,
      "card-1",
      first.calibration,
      tokenizer.count,
    );

    expect(grown.exact).toBe(false);
    expect(grown.tokens).toBe(40);
    expect(grown.calibration).toBe(first.calibration);
    expect(tokenizer.calls).toHaveLength(1);
  });

  it("re-calibrates once text grows by the live threshold", () => {
    const tokenizer = counter();
    const first = estimateLiveTokenCount("hello", "card-1", null, tokenizer.count);
    const grown = estimateLiveTokenCount(
      `hello${"x".repeat(1000)}`,
      "card-1",
      first.calibration,
      tokenizer.count,
    );

    expect(grown.exact).toBe(true);
    expect(grown.tokens).toBe(2010);
    expect(grown.calibration).toEqual({ cardId: "card-1", chars: 1005, tokens: 2010 });
    expect(tokenizer.calls).toHaveLength(2);
  });

  it("resets calibration for a new card id", () => {
    const tokenizer = counter();
    const previous: LiveTokenCalibration = { cardId: "card-1", chars: 200, tokens: 80 };
    const result = estimateLiveTokenCount("new text", "card-2", previous, tokenizer.count);

    expect(result.exact).toBe(true);
    expect(result.calibration.cardId).toBe("card-2");
    expect(tokenizer.calls).toEqual(["new text"]);
  });

  it("keeps tokenizer calls bucketed across many streaming chunks", () => {
    const tokenizer = counter();
    let calibration: LiveTokenCalibration | null = null;

    for (let chunk = 1; chunk <= 100; chunk++) {
      const result = estimateLiveTokenCount(
        "x".repeat(chunk * 100),
        "card-1",
        calibration,
        tokenizer.count,
      );
      calibration = result.calibration;
    }

    expect(tokenizer.calls.length).toBeLessThanOrEqual(20);
    expect(tokenizer.calls.length).toBeGreaterThan(1);
    expect(tokenizer.calls.at(-1)).toHaveLength(9100);
  });
});
