import { describe, expect, it, vi } from "vitest";
import { lazy } from "../src/core/lazy.js";

describe("lazy", () => {
  it("invokes the loader exactly once across many calls", async () => {
    const loader = vi.fn(async () => ({ value: 42 }));
    const get = lazy(loader);
    const [a, b, c] = await Promise.all([get(), get(), get()]);
    expect(loader).toHaveBeenCalledTimes(1);
    expect(a).toBe(b);
    expect(b).toBe(c);
    expect(a.value).toBe(42);
  });

  it("returns the same promise on every call", () => {
    const get = lazy(async () => "x");
    expect(get()).toBe(get());
  });

  it("propagates a loader rejection on every call", async () => {
    const err = new Error("boom");
    const get = lazy(async () => {
      throw err;
    });
    await expect(get()).rejects.toBe(err);
    await expect(get()).rejects.toBe(err);
  });
});
