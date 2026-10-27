import { describe, expect, it, vi } from "vitest";
import { STORAGE_TALLY_TTL, cachedStorageTotal, storageTotal } from "./storageTally";

const port = (bytes = 100) => ({ storage: vi.fn(async () => ({ roots: [{ bytes: bytes - 40 }, { bytes: 40 }, {}] })) });

describe("the storage total on the settings list", () => {
  it("walks the disk once for as long as the answer is fresh", async () => {
    const p = port();
    let now = 1_000;
    expect(await storageTotal(p as never, () => now)).toBe(100);
    now += STORAGE_TALLY_TTL - 1;
    expect(await storageTotal(p as never, () => now)).toBe(100);
    expect(p.storage).toHaveBeenCalledTimes(1);
  });

  it("walks again once the answer has gone stale", async () => {
    const p = port();
    let now = 1_000;
    await storageTotal(p as never, () => now);
    now += STORAGE_TALLY_TTL + 1;
    await storageTotal(p as never, () => now);
    expect(p.storage).toHaveBeenCalledTimes(2);
  });

  it("shares one walk between callers that ask while it runs", async () => {
    const p = port();
    const [a, b] = await Promise.all([storageTotal(p as never, () => 5), storageTotal(p as never, () => 5)]);
    expect([a, b]).toEqual([100, 100]);
    expect(p.storage).toHaveBeenCalledTimes(1);
  });

  it("does not remember a failure", async () => {
    const p = { storage: vi.fn().mockRejectedValueOnce(new Error("busy")).mockResolvedValue({ roots: [{ bytes: 7 }] }) };
    await expect(storageTotal(p as never, () => 1)).rejects.toThrow("busy");
    expect(await storageTotal(p as never, () => 2)).toBe(7);
    expect(p.storage).toHaveBeenCalledTimes(2);
  });

  it("keeps a separate answer for each port", async () => {
    const a = port(10);
    const b = port(20);
    expect(await storageTotal(a as never, () => 1)).toBe(10);
    expect(await storageTotal(b as never, () => 1)).toBe(20);
  });

  it("can say what it already knows without asking the disk", async () => {
    const p = port();
    expect(cachedStorageTotal(p, () => 1)).toBeUndefined();
    await storageTotal(p as never, () => 1);
    expect(cachedStorageTotal(p, () => 2)).toBe(100);
    expect(cachedStorageTotal(p, () => STORAGE_TALLY_TTL + 2)).toBeUndefined();
  });
});
