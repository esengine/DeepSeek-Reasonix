import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LruCache, TtlLruCache } from "../src/core/lru.js";

describe("LruCache", () => {
  it("returns undefined for an unknown key", () => {
    const c = new LruCache<string, number>(4);
    expect(c.get("missing")).toBeUndefined();
  });

  it("stores and retrieves values", () => {
    const c = new LruCache<string, number>(4);
    c.set("a", 1);
    expect(c.get("a")).toBe(1);
  });

  it("evicts the least-recently-used key past the limit", () => {
    const c = new LruCache<string, number>(2);
    c.set("a", 1);
    c.set("b", 2);
    c.set("c", 3);
    expect(c.get("a")).toBeUndefined();
    expect(c.get("b")).toBe(2);
    expect(c.get("c")).toBe(3);
  });

  it("get promotes a key so it survives the next eviction", () => {
    const c = new LruCache<string, number>(2);
    c.set("a", 1);
    c.set("b", 2);
    c.get("a"); // touch a — b is now oldest
    c.set("c", 3);
    expect(c.get("b")).toBeUndefined();
    expect(c.get("a")).toBe(1);
    expect(c.get("c")).toBe(3);
  });

  it("set on an existing key refreshes its position and value", () => {
    const c = new LruCache<string, number>(2);
    c.set("a", 1);
    c.set("b", 2);
    c.set("a", 99);
    c.set("c", 3);
    expect(c.get("b")).toBeUndefined();
    expect(c.get("a")).toBe(99);
  });

  it("clear empties the cache", () => {
    const c = new LruCache<string, number>(4);
    c.set("a", 1);
    c.set("b", 2);
    c.clear();
    expect(c.size).toBe(0);
    expect(c.get("a")).toBeUndefined();
  });
});

describe("TtlLruCache", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns a fresh value before the TTL elapses", () => {
    const c = new TtlLruCache<string, number>(4, 1000);
    c.set("a", 1);
    vi.advanceTimersByTime(500);
    expect(c.get("a")).toBe(1);
  });

  it("treats a stale entry as a miss", () => {
    const c = new TtlLruCache<string, number>(4, 1000);
    c.set("a", 1);
    vi.advanceTimersByTime(1500);
    expect(c.get("a")).toBeUndefined();
  });

  it("set refreshes the TTL of an existing key", () => {
    const c = new TtlLruCache<string, number>(4, 1000);
    c.set("a", 1);
    vi.advanceTimersByTime(800);
    c.set("a", 2);
    vi.advanceTimersByTime(500); // total 1300 since first set, 500 since refresh
    expect(c.get("a")).toBe(2);
  });

  it("preserves the underlying LRU eviction order", () => {
    const c = new TtlLruCache<string, number>(2, 1000);
    c.set("a", 1);
    c.set("b", 2);
    c.set("c", 3);
    expect(c.get("a")).toBeUndefined();
    expect(c.get("b")).toBe(2);
    expect(c.get("c")).toBe(3);
  });
});
