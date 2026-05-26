import { describe, expect, it, vi } from "vitest";
import { DeepSeekClient } from "../src/client.js";

function makeFetch(status: number, body: unknown) {
  return vi.fn(
    async () =>
      new Response(typeof body === "string" ? body : JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
  ) as unknown as typeof fetch;
}

describe("DeepSeekClient.listModels", () => {
  it("parses the OpenAI-style model list", async () => {
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: makeFetch(200, {
        object: "list",
        data: [
          { id: "deepseek-chat", object: "model", owned_by: "deepseek" },
          { id: "deepseek-reasoner", object: "model", owned_by: "deepseek" },
        ],
      }),
    });
    const list = await client.listModels();
    expect(list).not.toBeNull();
    expect(list!.data.map((m) => m.id)).toEqual(["deepseek-chat", "deepseek-reasoner"]);
  });

  it("returns null on non-2xx (bad key / offline)", async () => {
    const client = new DeepSeekClient({
      apiKey: "sk-bad",
      fetch: makeFetch(401, { error: "unauthorized" }),
    });
    expect(await client.listModels()).toBeNull();
  });

  it("returns null on malformed payload", async () => {
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: makeFetch(200, { whatever: "not a list" }),
    });
    expect(await client.listModels()).toBeNull();
  });

  it("returns null when fetch throws (network error)", async () => {
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: vi.fn(async () => {
        throw new Error("ECONNREFUSED");
      }) as unknown as typeof fetch,
    });
    expect(await client.listModels()).toBeNull();
  });

  it("sends the bearer token header", async () => {
    const spy = vi.fn(
      async () =>
        new Response(JSON.stringify({ object: "list", data: [] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    const client = new DeepSeekClient({
      apiKey: "sk-xyz",
      fetch: spy as unknown as typeof fetch,
    });
    await client.listModels();
    const [, init] = spy.mock.calls[0]!;
    expect((init as RequestInit).method).toBe("GET");
    const headers = (init as RequestInit).headers as Record<string, string>;
    expect(headers.Authorization).toBe("Bearer sk-xyz");
  });
});

describe("DeepSeekClient rateLimit", () => {
  it("waits between chat requests when rpm is configured", async () => {
    vi.useFakeTimers();
    const spy = vi.fn(
      async () =>
        new Response(JSON.stringify({ choices: [{ message: { content: "ok" } }] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: spy as unknown as typeof fetch,
      rateLimit: { rpm: 30 },
    });
    try {
      await client.chat({ model: "deepseek-chat", messages: [] });
      const second = client.chat({ model: "deepseek-chat", messages: [] });
      await Promise.resolve();
      expect(spy).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(1999);
      expect(spy).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(1);
      await second;
      expect(spy).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("DeepSeekClient usage parsing", () => {
  it("parses Ollama native top-level token metrics", async () => {
    const client = new DeepSeekClient({
      apiKey: "ollama",
      fetch: makeFetch(200, {
        model: "gpt-oss:20b",
        message: { role: "assistant", content: "ok" },
        done: true,
        prompt_eval_count: 42,
        eval_count: 7,
      }),
    });
    const resp = await client.chat({ model: "gpt-oss:20b", messages: [] });
    expect(resp.usage.promptTokens).toBe(42);
    expect(resp.usage.completionTokens).toBe(7);
    expect(resp.usage.totalTokens).toBe(49);
    expect(resp.usage.promptCacheMissTokens).toBe(42);
  });
});

describe("DeepSeekClient request serialization", () => {
  it("replaces lone UTF-16 surrogates before sending JSON", async () => {
    let sentBody = "";
    const spy = vi.fn(async (_url: unknown, init: unknown) => {
      sentBody = String((init as RequestInit).body ?? "");
      return new Response(JSON.stringify({ choices: [{ message: { content: "ok" } }] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: spy as unknown as typeof fetch,
    });

    await client.chat({
      model: "deepseek-chat",
      messages: [{ role: "user", content: `bad ${String.fromCharCode(0xd800)} text` }],
    });

    expect(sentBody).not.toMatch(/\\ud[89ab][0-9a-f]{2}/i);
    expect(JSON.parse(sentBody).messages[0].content).toBe("bad \uFFFD text");
  });
});
