import { afterEach, describe, expect, it, vi } from "vitest";
import { DeepSeekClient } from "../src/client.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";

function abortableNeverFetch(): typeof fetch {
  return vi.fn((_url: unknown, init: { signal?: AbortSignal } | undefined) => {
    const signal = init?.signal;
    return new Promise<Response>((_resolve, reject) => {
      if (signal?.aborted) {
        reject(new Error("aborted"));
        return;
      }
      signal?.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
    });
  }) as unknown as typeof fetch;
}

function seedTurns(loop: CacheFirstLoop, n: number): void {
  for (let i = 0; i < n; i++) {
    loop.log.append({
      role: "user",
      content: `question ${i}: ${"context padding for fold timeout regression ".repeat(8)}`,
    });
    loop.log.append({
      role: "assistant",
      content: `answer ${i}: ${"more context padding for fold timeout regression ".repeat(8)}`,
    });
  }
}

describe("ContextManager fold timeout", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("fails open when the summary request hangs", async () => {
    vi.useFakeTimers();
    const client = new DeepSeekClient({ apiKey: "sk-test", fetch: abortableNeverFetch() });
    const loop = new CacheFirstLoop({
      client,
      prefix: new ImmutablePrefix({ system: "s" }),
      stream: false,
    });
    seedTurns(loop, 6);
    const beforeMessages = loop.log.length;

    const resultPromise = loop.compactHistory({ keepRecentTokens: 40 });
    await vi.advanceTimersByTimeAsync(15_000);

    const result = await Promise.race([resultPromise, Promise.resolve("still-pending" as const)]);
    expect(result).not.toBe("still-pending");
    expect(result).toMatchObject({
      folded: false,
      beforeMessages,
      afterMessages: beforeMessages,
      summaryChars: 0,
    });
    expect(loop.log.length).toBe(beforeMessages);
  });
});
