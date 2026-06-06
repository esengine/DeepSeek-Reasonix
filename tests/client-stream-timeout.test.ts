import { describe, expect, it, vi } from "vitest";
import { DeepSeekClient } from "../src/client.js";

/** Mock fetch that returns a streaming body which never completes until
 *  the request signal aborts. Sends one SSE keep-alive comment first so
 *  the response headers flush and the consumer enters the read loop. */
function hangingStreamFetch(): { fetch: typeof fetch; calls: () => number } {
  let count = 0;
  const spy = vi.fn(async (_url: unknown, init: unknown) => {
    count++;
    const reqSignal = (init as RequestInit).signal as AbortSignal | null;
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode(": keep-alive\n\n"));
        if (!reqSignal) return;
        if (reqSignal.aborted) {
          controller.error(reqSignal.reason);
          return;
        }
        reqSignal.addEventListener(
          "abort",
          () => {
            try {
              controller.error(reqSignal.reason);
            } catch {
              /* controller already closed */
            }
          },
          { once: true },
        );
      },
    });
    return new Response(body, {
      status: 200,
      headers: { "Content-Type": "text/event-stream" },
    });
  });
  return { fetch: spy as unknown as typeof fetch, calls: () => count };
}

/** Mock fetch whose JSON body promise never resolves until the request
 *  signal aborts. Used to cover the non-streaming chat() path. */
function hangingJsonFetch(): typeof fetch {
  const spy = vi.fn(async (_url: unknown, init: unknown) => {
    const reqSignal = (init as RequestInit).signal as AbortSignal | null;
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        if (!reqSignal) return;
        if (reqSignal.aborted) {
          controller.error(reqSignal.reason);
          return;
        }
        reqSignal.addEventListener(
          "abort",
          () => {
            try {
              controller.error(reqSignal.reason);
            } catch {
              /* controller already closed */
            }
          },
          { once: true },
        );
      },
    });
    return new Response(body, {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  });
  return spy as unknown as typeof fetch;
}

describe("DeepSeekClient.stream() timeout with caller signal (issue #1535)", () => {
  it("aborts the stream when timeoutMs elapses even if the caller passed a signal", async () => {
    const { fetch } = hangingStreamFetch();
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch,
      timeoutMs: 50,
      retry: { maxAttempts: 1 },
    });

    const callerCtrl = new AbortController();
    const consume = async () => {
      for await (const _chunk of client.stream({
        model: "deepseek-chat",
        messages: [{ role: "user", content: "hi" }],
        signal: callerCtrl.signal,
      })) {
        /* drain */
      }
    };

    await expect(consume()).rejects.toThrow(/timed out/i);
    expect(callerCtrl.signal.aborted).toBe(false);
  });

  it("caller's signal still aborts the stream", async () => {
    const { fetch } = hangingStreamFetch();
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch,
      timeoutMs: 60_000,
      retry: { maxAttempts: 1 },
    });

    const callerCtrl = new AbortController();
    const consume = async () => {
      for await (const _chunk of client.stream({
        model: "deepseek-chat",
        messages: [{ role: "user", content: "hi" }],
        signal: callerCtrl.signal,
      })) {
        /* drain */
      }
    };

    const promise = consume();
    setTimeout(() => callerCtrl.abort(new Error("user pressed esc")), 30);
    await expect(promise).rejects.toThrow(/user pressed esc/);
  });
});

describe("DeepSeekClient.chat() timeout with caller signal", () => {
  it("aborts the request when timeoutMs elapses even if the caller passed a signal", async () => {
    const client = new DeepSeekClient({
      apiKey: "sk-test",
      fetch: hangingJsonFetch(),
      timeoutMs: 50,
      retry: { maxAttempts: 1 },
    });

    const callerCtrl = new AbortController();
    await expect(
      client.chat({
        model: "deepseek-chat",
        messages: [{ role: "user", content: "hi" }],
        signal: callerCtrl.signal,
      }),
    ).rejects.toThrow(/timed out/i);
    expect(callerCtrl.signal.aborted).toBe(false);
  });
});
