/** Queue for user steering messages while busy — tracks, auto-dismisses, restores. App.tsx consumes it. */

import { useCallback, useEffect, useRef, useState } from "react";

// Types

export interface QueuedMessage {
  text: string;
  enqueuedAt: number;
}

// Constants

/** How long a queued message sits before auto-dismissing (matches edit-undo convention). */
export const QUEUE_DISMISS_MS = 5_000;

// Pure helpers (testable without React)

/** Add a message to the queue. Rejects empty/whitespace. Returns the new queue. */
export function addMessage(
  queue: QueuedMessage[],
  text: string,
  now: number = Date.now(),
): { queue: QueuedMessage[]; rejected: boolean } {
  if (!text.trim()) return { queue, rejected: true };
  return {
    queue: [...queue, { text: text.trim(), enqueuedAt: now }],
    rejected: false,
  };
}

/** Remove (pop) the last message from the queue. Returns it + the new queue, or null if empty. */
export function popMessage(
  queue: QueuedMessage[],
): { message: QueuedMessage; queue: QueuedMessage[] } | null {
  if (queue.length === 0) return null;
  const last = queue[queue.length - 1]!;
  return { message: last, queue: queue.slice(0, -1) };
}

/** Remove all messages from the queue. */
export function clearQueue(): QueuedMessage[] {
  return [];
}

/** Filter out messages that have expired based on `since` timestamp. */
export function expireMessages(
  queue: QueuedMessage[],
  ttlMs: number = QUEUE_DISMISS_MS,
  now: number = Date.now(),
): QueuedMessage[] {
  return queue.filter((m) => now - m.enqueuedAt < ttlMs);
}

/** Time remaining before the newest message expires (0 if queue empty). */
export function remainingMs(
  queue: QueuedMessage[],
  ttlMs: number = QUEUE_DISMISS_MS,
  now: number = Date.now(),
): number {
  if (queue.length === 0) return 0;
  const latest = queue[queue.length - 1]!;
  return Math.max(0, ttlMs - (now - latest.enqueuedAt));
}

// React hook

export function useMessageQueue(ttlMs: number = QUEUE_DISMISS_MS): {
  /** Current queued messages (not yet consumed by the loop). */
  queue: QueuedMessage[];
  /** Number of queued messages (convenience). */
  count: number;
  /** Add a message to the queue. Returns true if accepted, false if rejected (empty). */
  enqueue: (text: string) => boolean;
  /** Pop the last message off the queue (restore to input buffer). */
  dequeue: () => string | null;
  /** Clear all queued messages. */
  clear: () => void;
} {
  const [queue, setQueue] = useState<QueuedMessage[]>([]);
  const expiryRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Auto-dismiss timer: when queue transitions from empty → non-empty,
  // start a timer that removes the newest message after ttlMs.
  useEffect(() => {
    if (queue.length === 0) {
      if (expiryRef.current) {
        clearTimeout(expiryRef.current);
        expiryRef.current = null;
      }
      return;
    }
    // Schedule expiry for the latest message
    const latest = queue[queue.length - 1]!;
    const elapsed = Date.now() - latest.enqueuedAt;
    const remaining = Math.max(0, ttlMs - elapsed);
    if (remaining <= 0) {
      // Already expired — pop it
      setQueue((prev) => expireMessages(prev, ttlMs));
      return;
    }
    expiryRef.current = setTimeout(() => {
      setQueue((prev) => {
        const expired = expireMessages(prev, ttlMs);
        // If nothing was removed, nothing to do
        if (expired.length === prev.length) return prev;
        // The latest message expired: return the filtered queue
        return expired;
      });
    }, remaining);
    return () => {
      if (expiryRef.current) clearTimeout(expiryRef.current);
    };
  }, [queue, ttlMs]);

  const enqueue = useCallback(
    (text: string): boolean => {
      const { queue: next, rejected } = addMessage(queue, text);
      if (rejected) return false;
      setQueue(next);
      return true;
    },
    [queue],
  );

  const dequeue = useCallback((): string | null => {
    const result = popMessage(queue);
    if (!result) return null;
    setQueue(result.queue);
    return result.message.text;
  }, [queue]);

  const clear = useCallback(() => {
    setQueue([]);
  }, []);

  return { queue, count: queue.length, enqueue, dequeue, clear };
}
