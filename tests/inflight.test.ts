/** InflightSet — finally-driven cleanup contract. */

import { describe, expect, it, vi } from "vitest";
import { InflightSet } from "../src/core/inflight.js";

describe("InflightSet", () => {
  it("add / has / delete round-trips", () => {
    const s = new InflightSet();
    expect(s.has("a")).toBe(false);
    s.add("a");
    expect(s.has("a")).toBe(true);
    expect(s.size).toBe(1);
    s.delete("a");
    expect(s.has("a")).toBe(false);
    expect(s.size).toBe(0);
  });

  it("add is idempotent — re-adding the same id is a no-op + does not re-notify", () => {
    const s = new InflightSet();
    const sub = vi.fn();
    s.subscribe(sub);
    s.add("a");
    s.add("a");
    s.add("a");
    expect(sub).toHaveBeenCalledTimes(1);
  });

  it("delete on a missing id does not notify", () => {
    const s = new InflightSet();
    const sub = vi.fn();
    s.subscribe(sub);
    s.delete("never-added");
    expect(sub).not.toHaveBeenCalled();
  });

  it("subscribe fires on add and on delete", () => {
    const s = new InflightSet();
    const sub = vi.fn();
    s.subscribe(sub);
    s.add("a");
    s.add("b");
    s.delete("a");
    expect(sub).toHaveBeenCalledTimes(3);
  });

  it("subscribe returns an unsubscribe function", () => {
    const s = new InflightSet();
    const sub = vi.fn();
    const unsub = s.subscribe(sub);
    s.add("a");
    expect(sub).toHaveBeenCalledTimes(1);
    unsub();
    s.add("b");
    expect(sub).toHaveBeenCalledTimes(1);
  });

  it("listener errors do not propagate or break further notifications", () => {
    const s = new InflightSet();
    s.subscribe(() => {
      throw new Error("boom");
    });
    const ok = vi.fn();
    s.subscribe(ok);
    expect(() => s.add("a")).not.toThrow();
    expect(ok).toHaveBeenCalledTimes(1);
  });

  it("clear empties the set and notifies once", () => {
    const s = new InflightSet();
    s.add("a");
    s.add("b");
    const sub = vi.fn();
    s.subscribe(sub);
    s.clear();
    expect(s.size).toBe(0);
    expect(s.has("a")).toBe(false);
    expect(s.has("b")).toBe(false);
    expect(sub).toHaveBeenCalledTimes(1);
  });

  it("clear on an empty set is a no-op + does not notify", () => {
    const s = new InflightSet();
    const sub = vi.fn();
    s.subscribe(sub);
    s.clear();
    expect(sub).not.toHaveBeenCalled();
  });

  it("models the finally contract — id is removed even when work throws", async () => {
    const s = new InflightSet();
    const work = async () => {
      s.add("job-1");
      try {
        throw new Error("simulated tool failure");
      } finally {
        s.delete("job-1");
      }
    };
    await expect(work()).rejects.toThrow("simulated tool failure");
    // The whole point of the refactor: regardless of how the work exits,
    // the inflight bit is gone, so the spinner stops.
    expect(s.has("job-1")).toBe(false);
    expect(s.size).toBe(0);
  });

  it("models the finally contract — id is removed when work is aborted mid-flight", async () => {
    const s = new InflightSet();
    const ctl = new AbortController();
    const work = async (signal: AbortSignal) => {
      s.add("abortable");
      try {
        // Simulated tool that hangs until the signal fires.
        await new Promise<void>((resolve, reject) => {
          if (signal.aborted) {
            reject(new Error("aborted before await"));
            return;
          }
          signal.addEventListener("abort", () => reject(new Error("aborted")), { once: true });
        });
      } finally {
        s.delete("abortable");
      }
    };
    const promise = work(ctl.signal);
    expect(s.has("abortable")).toBe(true);
    ctl.abort();
    await expect(promise).rejects.toThrow();
    expect(s.has("abortable")).toBe(false);
  });

  it("parallel adds with concurrent settle — every id leaves the set on completion", async () => {
    const s = new InflightSet();
    const work = async (id: string, ms: number, fail: boolean) => {
      s.add(id);
      try {
        await new Promise((r) => setTimeout(r, ms));
        if (fail) throw new Error(`${id} failed`);
      } finally {
        s.delete(id);
      }
    };
    const settled = await Promise.allSettled([
      work("a", 5, false),
      work("b", 1, true),
      work("c", 3, false),
    ]);
    expect(settled.map((r) => r.status)).toEqual(["fulfilled", "rejected", "fulfilled"]);
    expect(s.size).toBe(0);
  });
});
