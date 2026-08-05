import { describe, expect, it } from "vitest";
import type { Member } from "./env";
import { assertCanFlag, assertCanInteract } from "./antispam";

function member(overrides: Partial<Member> = {}): Member {
  return {
    email: "alice@example.test",
    handle: "alice",
    emailVerified: true,
    trust: 0,
    role: "member",
    silencedUntil: null,
    ...overrides,
  };
}

describe("community interaction gates", () => {
  it("rejects reactions and reports from unverified identities", () => {
    expect(() => assertCanInteract(member({ emailVerified: false }))).toThrowError(/Confirm your email/);
    expect(() => assertCanFlag(member({ emailVerified: false }), "bob@example.test")).toThrowError(/Confirm your email/);
  });

  it("rejects self-reporting", () => {
    expect(() => assertCanFlag(member(), "alice@example.test")).toThrowError(/own post/);
  });
});
