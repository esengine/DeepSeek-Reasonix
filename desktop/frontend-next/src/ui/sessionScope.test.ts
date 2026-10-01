import { describe, expect, it } from "vitest";
import { inScope } from "./sessionScope";

const live = { path: "/a.jsonl", name: "a", archived: false };
const archived = { path: "/b.jsonl", name: "b", archived: true };

describe("which list a conversation is in", () => {
  it("hides an archived conversation from the everyday list", () => {
    expect(inScope(archived, "all", false, false, false)).toBe(false);
  });
  it("finds it when searching, and only in the full list", () => {
    expect(inScope(archived, "all", true, false, false)).toBe(true);
    expect(inScope(archived, "pinned", true, false, true)).toBe(false);
    expect(inScope(archived, "live", true, true, false)).toBe(false);
  });
  it("lists it under Archived and nothing else there", () => {
    expect(inScope(archived, "archived", false, false, false)).toBe(true);
    expect(inScope(live, "archived", false, false, false)).toBe(false);
  });
});
