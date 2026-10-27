// @vitest-environment jsdom
import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import type { Item } from "../state/session";
import { useBlocks, useBlockKeys } from "./blocks";

afterEach(cleanup);
const turns = (prefix: string, count: number): Item[] => Array.from({ length: count }, (_, i): Item[] => [
  { t: "user", id: `${prefix}-u${i}`, text: "question" }, { t: "say", id: `${prefix}-a${i}`, text: "answer", done: true },
]).flat();

it("preserves existing mounting units when an older page is prepended", () => {
  const items = turns("tail", 60);
  const { result, rerender } = renderHook(({ items, revision, cut }) => useBlocks(items, cut, revision), { initialProps: { items, revision: 1, cut: items.length } });
  const prior = result.current;
  const combined = [...turns("head", 10), ...items];
  rerender({ items: combined, revision: 2, cut: combined.length });
  for (const block of prior) expect(result.current).toContain(block);
  expect(result.current.flat()).toEqual(combined);
  rerender({ items: combined, revision: 3, cut: combined.length - 1 });
  expect(result.current.flat()).toEqual(combined.slice(0, -1));
});

it("keeps an appended continuation in the same authored-turn block", () => {
  const items: Item[] = [{ t: "user", id: "user", text: "question" }, ...Array.from({ length: 50 }, (_, i): Item => ({ t: "say", id: `answer-${i}`, text: "continuation", done: true }))];
  const { result, rerender } = renderHook(({ items, revision }) => useBlocks(items, items.length, revision), { initialProps: { items, revision: 1 } });
  expect(result.current).toHaveLength(1);
  const continued: Item[] = [...items, { t: "say", id: "continued", text: "still the same turn", done: true }];
  rerender({ items: continued, revision: 2 });
  expect(result.current).toHaveLength(1);
  expect(result.current[0]).toEqual(continued);
});

it("retains a live tail's mounting identity when late history joins its block", () => {
  const tail: Item[] = [{ t: "say", id: "live", text: "live tail", done: true }];
  const { result, rerender } = renderHook(({ blocks }) => useBlockKeys(blocks), { initialProps: { blocks: [tail] } });
  const key = result.current[0];
  const restored = [[...turns("history", 1), ...tail]];
  rerender({ blocks: restored });
  expect(result.current).toEqual([key]);
  const stable = result.current;
  rerender({ blocks: restored });
  expect(result.current).toBe(stable);
  rerender({ blocks: [turns("older", 1), [...turns("history", 1), ...tail]] });
  expect(result.current[1]).toBe(key);
  expect(new Set(result.current).size).toBe(result.current.length);
});
