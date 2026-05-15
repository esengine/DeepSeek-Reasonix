import { describe, expect, it } from "vitest";
import { countTokens, encode, estimateConversationTokens } from "../src/tokenizer.js";

describe("DeepSeek V4 tokenizer — golden cases", () => {
  // These IDs were captured from the pure-TS port running against the
  // bundled `data/deepseek-tokenizer.json.gz`. They match what DeepSeek's
  // official Python tokenizer produces (HF LlamaTokenizerFast on the
  // same tokenizer.json). If a case regresses, check that the data
  // file wasn't accidentally truncated or the pre_tokenizer Sequence
  // wasn't reordered.
  it("empty string is zero tokens", () => {
    expect(encode("")).toEqual([]);
    expect(countTokens("")).toBe(0);
  });

  it("ASCII words tokenize compactly", () => {
    expect(encode("Hello!")).toEqual([19923, 3]);
    expect(encode("Hello, world!")).toEqual([19923, 14, 2058, 3]);
  });

  it("common CJK collocation is a single token", () => {
    expect(encode("你好")).toEqual([30594]);
  });

  it("CJK sentence splits on punctuation", () => {
    expect(encode("你好，世界！")).toEqual([30594, 303, 3427, 1175]);
  });

  it("digit run is isolated by the \\p{N}{1,3} pre-tokenizer rule", () => {
    // "1 + 1 = 2" → numbers get their own tokens; spaces/operators
    // fold into byte-level pieces.
    expect(encode("1 + 1 = 2")).toEqual([19, 940, 223, 19, 438, 223, 20]);
  });

  it("recognizes <think>/</think> as atomic added tokens", () => {
    // 128821 = <think>, 128822 = </think> per tokenizer.json added_tokens (V4).
    const ids = encode("<think>reasoning here</think>");
    expect(ids[0]).toBe(128821);
    expect(ids[ids.length - 1]).toBe(128822);
    expect(ids.length).toBe(5);
  });

  it("mixed English+CJK follows the right pre-tokenizer branches", () => {
    const ids = encode("mixed 中文 and english 混合");
    expect(ids).toEqual([122545, 223, 21134, 305, 33010, 223, 14769]);
  });

  it("round-trips a code snippet at a reasonable compression ratio", () => {
    const src = "function add(a, b) { return a + b; }";
    const n = countTokens(src);
    // 37 chars → expected ~12-14 tokens for a ByteLevel BPE trained on
    // code. Assert a loose band so a future tokenizer refresh (vocab
    // shift ±5%) doesn't break the test suite.
    expect(n).toBeGreaterThanOrEqual(10);
    expect(n).toBeLessThanOrEqual(16);
  });

  it("Chinese prose gets the expected ~0.6 tokens/char rate", () => {
    const text = "深度求索是一家专注于人工智能基础技术研究的公司";
    const n = countTokens(text);
    // 22 CJK chars → DeepSeek's doc claims ~0.6 tokens/char ≈ 13, our
    // V4 tokenizer's CJK compression is tighter; allow 8-16 as the
    // sanity range.
    expect(n).toBeGreaterThanOrEqual(8);
    expect(n).toBeLessThanOrEqual(16);
  });
});

describe("estimateConversationTokens", () => {
  it("sums content across roles", () => {
    const n = estimateConversationTokens([
      { content: "you are helpful" },
      { content: "你好" },
      { content: "Hello!" },
    ]);
    const manual = countTokens("you are helpful") + countTokens("你好") + countTokens("Hello!");
    expect(n).toBe(manual);
  });

  it("counts tool_calls JSON bytes when present", () => {
    const withCalls = estimateConversationTokens([
      {
        content: null,
        tool_calls: [{ id: "c1", function: { name: "read_file", arguments: '{"path":"a.ts"}' } }],
      },
    ]);
    // The tool_calls serialization itself has weight; should be > 0.
    expect(withCalls).toBeGreaterThan(0);
  });

  it("ignores missing / empty content without crashing", () => {
    expect(
      estimateConversationTokens([{ content: null }, { content: "" }, { content: undefined }]),
    ).toBe(0);
  });
});

describe("performance sanity", () => {
  it("tokenizes 10k chars of typical mixed content in under 200 ms", () => {
    const block = "Hello world! 你好 deepseek ".repeat(400); // ~9,600 chars
    const t0 = performance.now();
    const n = countTokens(block);
    const t1 = performance.now();
    expect(n).toBeGreaterThan(1000);
    expect(t1 - t0).toBeLessThan(200);
  });
});
