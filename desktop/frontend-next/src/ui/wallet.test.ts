import { describe, expect, it } from "vitest";
import { accountOf, since } from "./wallet";

const AT = Date.parse("2026-08-27T00:00:00Z");

describe("since", () => {
  // The coarsest unit still true: a wallet moves on the order of minutes, so
  // "37 seconds ago" would be precision nobody can act on.
  it.each([
    [30_000, "刚刚"],
    [90_000, "1 分钟前"],
    [59 * 60_000, "59 分钟前"],
    [90 * 60_000, "1 小时前"],
    [26 * 3600_000, "1 天前"],
  ])("renders %ims as %s", (ms, want) => {
    expect(since(new Date(AT).toISOString(), AT + ms)).toBe(want);
  });

  // A clock that disagrees with the kernel's must not render "-3 minutes ago".
  it("does not run backwards", () => {
    expect(since(new Date(AT).toISOString(), AT - 60_000)).toBe("刚刚");
  });

  it("says nothing about a date it cannot read", () => {
    expect(since("not a date", AT)).toBe("");
  });
});

describe("accountOf", () => {
  it("takes the provider half of a model ref", () => {
    expect(accountOf("deepseek/deepseek-v4-pro")).toBe("deepseek");
  });

  // No ref yet is a wallet with no account name, not a wallet labelled "".
  it.each([undefined, ""])("has no name for %p", (ref) => {
    expect(accountOf(ref)).toBe("");
  });
});
