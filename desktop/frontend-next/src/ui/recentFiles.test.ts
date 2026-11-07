// @vitest-environment jsdom
import { beforeEach, expect, it } from "vitest";
import { readRecentFiles, rememberFile } from "./recentFiles";

beforeEach(() => localStorage.clear());

it("keeps the last few files opened in a project, newest first, without repeats", () => {
  for (const f of ["a.ts", "b.ts", "c.ts", "a.ts"]) rememberFile("/w", f);
  expect(readRecentFiles("/w")).toEqual(["a.ts", "c.ts", "b.ts"]);
});

it("keeps projects apart and caps the list", () => {
  for (let i = 0; i < 12; i++) rememberFile("/w", `f${i}.ts`);
  expect(readRecentFiles("/w")).toHaveLength(6);
  expect(readRecentFiles("/other")).toEqual([]);
});
