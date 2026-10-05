import { describe, expect, it, vi } from "vitest";
import { trailing } from "./trailing";

// A save whose answer the test decides when to give, so the ordering this
// module exists for can be written down rather than raced for.
function held<T>() {
  const calls: T[] = [];
  const waiting: ((v: T) => void)[] = [];
  const rejects: ((e: unknown) => void)[] = [];
  const save = (v: T) =>
    new Promise<T>((resolve, reject) => {
      calls.push(v);
      waiting.push(resolve);
      rejects.push(reject);
    });
  // Answers land through a microtask, so a settled promise's .then has run by
  // the time the caller looks.
  const answer = async (i: number, v: T) => {
    waiting[i](v);
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  };
  const refuse = async (i: number, e: unknown) => {
    rejects[i](e);
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  };
  return { calls, save, answer, refuse };
}

describe("trailing writes", () => {
  it("sends the first value at once — a single click must not wait on a timer", () => {
    const { calls, save } = held<number>();
    const write = trailing(save, () => {}, () => {});
    write(1);
    expect(calls).toEqual([1]);
  });

  it("collapses a whole drag into one follow-up write", async () => {
    const { calls, save, answer } = held<number>();
    const write = trailing(save, () => {}, () => {});
    write(1);
    for (const v of [2, 3, 4, 5]) write(v);
    expect(calls).toEqual([1]);
    await answer(0, 1);
    expect(calls).toEqual([1, 5]);
  });

  it("keeps the newest of what it collapsed, not the first", async () => {
    const { calls, save, answer } = held<string>();
    const write = trailing(save, () => {}, () => {});
    write("a");
    write("b");
    write("c");
    await answer(0, "a");
    expect(calls[1]).toBe("c");
  });

  it("adopts the answer, because the kernel clamps what the slider sent", async () => {
    const { save, answer } = held<number>();
    const seen: number[] = [];
    const write = trailing(save, (v) => seen.push(v), () => {});
    write(9);
    await answer(0, 1);
    expect(seen).toEqual([1]);
  });

  it("does not adopt an answer the control has already moved past", async () => {
    const { save, answer } = held<number>();
    const seen: number[] = [];
    const write = trailing(save, (v) => seen.push(v), () => {});
    write(1);
    write(2); // moved while the first write was away
    await answer(0, 1);
    // Adopting 1 here is the snap-back: the thumb is at 2 and the config is
    // about to be told so.
    expect(seen).toEqual([]);
    await answer(1, 2);
    expect(seen).toEqual([2]);
  });

  it("reports a failure and still writes what came after it", async () => {
    const { calls, save, answer, refuse } = held<number>();
    const failed = vi.fn();
    const write = trailing(save, () => {}, failed);
    write(1);
    write(2);
    await refuse(0, new Error("kernel said no"));
    expect(failed).toHaveBeenCalledTimes(1);
    expect(calls).toEqual([1, 2]);
    await answer(1, 2);
  });

  it("never has two writes in flight, however fast the caller moves", async () => {
    const { calls, save, answer } = held<number>();
    const write = trailing(save, () => {}, () => {});
    for (let i = 0; i < 50; i++) write(i);
    expect(calls).toEqual([0]);
    await answer(0, 0);
    expect(calls).toEqual([0, 49]);
    await answer(1, 49);
    expect(calls).toEqual([0, 49]);
  });
});
