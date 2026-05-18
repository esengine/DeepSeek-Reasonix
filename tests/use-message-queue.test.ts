/** useMessageQueue — queue management hook the App.tsx will use for user steering messages.
 *  Tests will fail until the hook is created at src/cli/ui/hooks/useMessageQueue.ts. */

import { describe, expect, it } from "vitest";
import {
  QUEUE_DISMISS_MS,
  addMessage,
  clearQueue,
  expireMessages,
  popMessage,
  remainingMs,
} from "../src/cli/ui/hooks/useMessageQueue.js";

// Pure function tests (no React mount needed)

describe("useMessageQueue — pure helpers", () => {
  // addMessage ----------------------------------------------------------

  it("addMessage appends trimmed text with a timestamp", () => {
    const { queue, rejected } = addMessage([], "look at src/", 1000);
    expect(rejected).toBe(false);
    expect(queue).toHaveLength(1);
    expect(queue[0]!.text).toBe("look at src/");
    expect(queue[0]!.enqueuedAt).toBe(1000);
  });

  it("addMessage rejects empty strings (trimmed)", () => {
    const { queue, rejected } = addMessage([], "   ", 1000);
    expect(rejected).toBe(true);
    expect(queue).toHaveLength(0);
  });

  it("addMessage rejects empty string", () => {
    const { queue, rejected } = addMessage([], "", 1000);
    expect(rejected).toBe(true);
    expect(queue).toHaveLength(0);
  });

  it("addMessage does NOT reject whitespace-surrounded text", () => {
    const { queue, rejected } = addMessage([], "  hello  ", 1000);
    expect(rejected).toBe(false);
    expect(queue[0]!.text).toBe("hello");
  });

  it("addMessage appends to an existing queue", () => {
    const existing = [{ text: "first", enqueuedAt: 100 }];
    const { queue } = addMessage(existing, "second", 200);
    expect(queue).toHaveLength(2);
    expect(queue[0]!.text).toBe("first");
    expect(queue[1]!.text).toBe("second");
  });

  // popMessage ----------------------------------------------------------

  it("popMessage returns null when queue is empty", () => {
    expect(popMessage([])).toBeNull();
  });

  it("popMessage removes and returns the last message", () => {
    const queue = [
      { text: "first", enqueuedAt: 100 },
      { text: "second", enqueuedAt: 200 },
    ];
    const result = popMessage(queue);
    expect(result).not.toBeNull();
    expect(result!.message.text).toBe("second");
    expect(result!.queue).toHaveLength(1);
    expect(result!.queue[0]!.text).toBe("first");
  });

  it("popMessage on single-item queue returns it and an empty queue", () => {
    const queue = [{ text: "only", enqueuedAt: 100 }];
    const result = popMessage(queue);
    expect(result!.message.text).toBe("only");
    expect(result!.queue).toEqual([]);
  });

  // clearQueue ----------------------------------------------------------

  it("clearQueue returns an empty array", () => {
    expect(clearQueue()).toEqual([]);
  });

  // expireMessages ------------------------------------------------------

  it("expireMessages removes messages older than ttl", () => {
    const queue = [
      { text: "old", enqueuedAt: 100 },
      { text: "fresh", enqueuedAt: 8000 },
    ];
    const filtered = expireMessages(queue, 5000, 10000);
    expect(filtered).toHaveLength(1);
    expect(filtered[0]!.text).toBe("fresh");
  });

  it("expireMessages keeps all messages within ttl", () => {
    const queue = [
      { text: "a", enqueuedAt: 6000 },
      { text: "b", enqueuedAt: 8000 },
    ];
    const filtered = expireMessages(queue, 5000, 10000);
    expect(filtered).toHaveLength(2);
  });

  it("expireMessages returns empty array when all are expired", () => {
    const queue = [
      { text: "dead", enqueuedAt: 0 },
      { text: "gone", enqueuedAt: 1000 },
    ];
    // Both are older than 5s when now=6000
    expect(expireMessages(queue, 5000, 6000)).toEqual([]);
  });

  // remainingMs ---------------------------------------------------------

  it("remainingMs returns 0 for empty queue", () => {
    expect(remainingMs([], 5000, 1000)).toBe(0);
  });

  it("remainingMs returns time until newest message expires", () => {
    const queue = [{ text: "x", enqueuedAt: 2000 }];
    expect(remainingMs(queue, 5000, 3000)).toBe(4000); // 5s - (3s-2s) = 4s
  });

  it("remainingMs clamps to 0 when past ttl", () => {
    const queue = [{ text: "x", enqueuedAt: 0 }];
    expect(remainingMs(queue, 5000, 10000)).toBe(0);
  });

  // QUEUE_DISMISS_MS ----------------------------------------------------

  it("QUEUE_DISMISS_MS is 5000 (matches edit-undo convention)", () => {
    expect(QUEUE_DISMISS_MS).toBe(5000);
  });
});
