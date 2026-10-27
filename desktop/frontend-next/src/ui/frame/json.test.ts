import { expect, it } from "vitest";
import { decodeFrameMessage } from "./bridge";
import { fitsJSON } from "./json";

it("counts the encoded JSON bytes and rejects non-JSON structured clones", () => {
  for (const value of [null, true, false, 1, -12.5, "界\n\"", ["a", 2], { a: "界" }]) {
    const bytes = new TextEncoder().encode(JSON.stringify(value)).length;
    expect(fitsJSON(value, bytes)).toBe(true);
    expect(fitsJSON(value, bytes - 1)).toBe(false);
  }
  const cyclic: unknown[] = [];
  cyclic.push(cyclic);
  for (const value of [cyclic, 1n, undefined, new Map(), new Set(), new Date(), NaN, Infinity, new Uint8Array(10)]) {
    expect(fitsJSON(value, 65536)).toBe(false);
  }
  const shared = { value: 1 };
  expect(fitsJSON([shared, shared], 100)).toBe(true);
});

it("bounds nesting, node count and UTF-8 action arguments", () => {
  let deep: unknown = 0;
  for (let n = 0; n < 100; n++) deep = [deep];
  expect(fitsJSON(deep, 65536)).toBe(false);
  expect(fitsJSON(Array(5000).fill(null), 65536)).toBe(false);
  expect(decodeFrameMessage({ type: "action", actionId: "run", args: { value: "界".repeat(342) } })).toBeNull();
  expect(decodeFrameMessage({ type: "action", actionId: "run", args: { value: "界".repeat(341) } })).not.toBeNull();
});
