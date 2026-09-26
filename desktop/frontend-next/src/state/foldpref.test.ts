import { describe, expect, it } from "vitest";
import { liveWork, startsOpen } from "./foldpref";
import type { Item } from "./session_types";

describe("startsOpen", () => {
  it("answers each mode from the part's state", () => {
    expect(startsOpen("folded", true, true)).toBe(false);
    expect(startsOpen("open")).toBe(true);
    expect(startsOpen("live", true)).toBe(true);
    expect(startsOpen("live", false)).toBe(false);
    expect(startsOpen("failed", false, true)).toBe(true);
    expect(startsOpen("failed", true, false)).toBe(false);
    expect(startsOpen("changed", false, false, true)).toBe(true);
    expect(startsOpen("changed", true, true, false)).toBe(false);
  });
});

describe("liveWork", () => {
  const tool = (id: string) => ({ t: "tool", id, running: false, children: [], tool: { id, name: "bash" } }) as unknown as Item;
  const user = (id: string) => ({ t: "user", id, text: "" }) as unknown as Item;

  it("holds the running turn's work and nothing before it", () => {
    const items = [user("u1"), tool("a"), user("u2"), tool("b"), tool("c")];
    expect([...liveWork(items, true)]).toEqual(["c", "b"]);
    expect(liveWork(items, false).size).toBe(0);
  });
});
