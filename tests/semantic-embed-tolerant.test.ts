import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { EmbeddingError, embedAll } from "../src/index/semantic/embedding.js";

describe("embedAll fault tolerance", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  function stubFetch(handler: (callIdx: number) => Promise<Response> | Response) {
    let callIdx = 0;
    globalThis.fetch = vi.fn(async () => {
      const r = await handler(callIdx++);
      return r;
    }) as unknown as typeof globalThis.fetch;
  }

  function jsonOk(embedding: number[]): Response {
    return new Response(JSON.stringify({ embedding }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }

  function jsonErr(status: number, body: unknown): Response {
    return new Response(typeof body === "string" ? body : JSON.stringify(body), {
      status,
      headers: { "content-type": "application/json" },
    });
  }

  it("returns null in the slot for a single failing chunk and continues", async () => {
    stubFetch((i) => {
      if (i === 1) {
        return jsonErr(500, { error: "the input length exceeds the context length" });
      }
      return jsonOk([1, 0, 0]);
    });

    const skipped: Array<{ index: number; err: unknown }> = [];
    const out = await embedAll(["a", "b", "c"], {
      onError: (index, err) => skipped.push({ index, err }),
    });
    expect(out).toHaveLength(3);
    expect(out[0]).toBeInstanceOf(Float32Array);
    expect(out[1]).toBeNull();
    expect(out[2]).toBeInstanceOf(Float32Array);
    expect(skipped).toHaveLength(1);
    expect(skipped[0]?.index).toBe(1);
  });

  it("aborts on signal but doesn't surface aborts as per-chunk errors", async () => {
    stubFetch(() => jsonOk([1, 0, 0]));
    const ac = new AbortController();
    ac.abort(new Error("user cancelled"));
    await expect(embedAll(["a", "b"], { signal: ac.signal })).rejects.toThrow(/aborted/);
  });

  it("returns all-null when every chunk fails", async () => {
    stubFetch(() => jsonErr(500, { error: "context length" }));
    const errors: number[] = [];
    const out = await embedAll(["a", "b"], {
      onError: (i) => errors.push(i),
    });
    expect(out).toEqual([null, null]);
    expect(errors).toEqual([0, 1]);
  });

  it("throws for openai-compatible batch failures instead of returning all-null", async () => {
    stubFetch(() => jsonErr(500, { error: "context length" }));
    const errors: number[] = [];
    await expect(
      embedAll(["a", "b"], {
        provider: "openai-compat",
        baseUrl: "https://api.example.com/v1",
        apiKey: "sk-openai1234567890abcd",
        model: "text-embedding-3-small",
        onError: (i) => errors.push(i),
      }),
    ).rejects.toBeInstanceOf(EmbeddingError);
    expect(errors).toEqual([]);
  });

  it("treats openai-compatible aborts as aborts", async () => {
    globalThis.fetch = vi.fn(async (_input, init) => {
      const signal = init?.signal;
      return await new Promise<Response>((_resolve, reject) => {
        signal?.addEventListener(
          "abort",
          () => reject(signal.reason ?? new DOMException("Aborted", "AbortError")),
          { once: true },
        );
      });
    }) as unknown as typeof globalThis.fetch;
    const ac = new AbortController();
    const promise = embedAll(["a"], {
      provider: "openai-compat",
      baseUrl: "https://api.example.com/v1",
      apiKey: "sk-openai1234567890abcd",
      model: "text-embedding-3-small",
      signal: ac.signal,
    });
    ac.abort(new Error("user cancelled"));
    await expect(promise).rejects.toThrow(/aborted/);
  });
});
