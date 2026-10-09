import { describe, expect, it } from "vitest";
import { singleFlight } from "./singleflight";

const gate = () => {
  let release: () => void = () => {};
  const done = new Promise<void>((resolve) => (release = resolve));
  return { done, release };
};

describe("singleFlight", () => {
  it("runs once for a lone call", async () => {
    let runs = 0;
    const go = singleFlight(async () => void runs++);
    await go();
    expect(runs).toBe(1);
  });

  it("folds calls made during a run into one follow-up that starts after the first settles", async () => {
    const gates = [gate(), gate()];
    const started: number[] = [];
    const go = singleFlight(() => {
      const n = started.length;
      started.push(n);
      return gates[n]?.done ?? Promise.resolve();
    });

    const first = go();
    const second = go();
    const third = go();
    expect(started).toEqual([0]);
    expect(second).toBe(third);

    gates[0].release();
    await first;
    await Promise.resolve();
    expect(started).toEqual([0, 1]);
    gates[1].release();
    await second;
    expect(started).toEqual([0, 1]);
  });

  it("starts a fresh run for a call after everything settled", async () => {
    let runs = 0;
    const go = singleFlight(async () => void runs++);
    await go();
    await go();
    expect(runs).toBe(2);
  });

  it("still runs the follow-up when the first run fails", async () => {
    let runs = 0;
    const go = singleFlight(async () => {
      if (++runs === 1) throw new Error("boom");
    });
    const first = go();
    const second = go();
    await expect(first).rejects.toThrow("boom");
    await expect(second).resolves.toBeUndefined();
    expect(runs).toBe(2);
  });
});
