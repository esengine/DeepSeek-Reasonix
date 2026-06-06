import { describe, expect, it } from "vitest";
import {
  decideQQAccess,
  describeQQAccess,
  normalizeQQAllowlist,
  redactQQOpenId,
} from "../src/qq/access.js";

describe("QQ access control", () => {
  it("binds the first sender for the current run when no persistent access rule exists", () => {
    const first = decideQQAccess({}, "openid-first");
    expect(first).toEqual({ accept: true, mode: "open", bindRuntime: true });

    const repeat = decideQQAccess({ runtimeBoundOpenId: "openid-first" }, "openid-first");
    expect(repeat).toEqual({ accept: true, mode: "runtime", bindRuntime: false });

    const other = decideQQAccess({ runtimeBoundOpenId: "openid-first" }, "openid-other");
    expect(other).toEqual({ accept: false, reason: "unauthorized" });
  });

  it("accepts the persistent owner and reject outsiders", () => {
    expect(decideQQAccess({ ownerOpenId: "owner-1" }, "owner-1")).toEqual({
      accept: true,
      mode: "owner",
      bindRuntime: false,
    });
    expect(decideQQAccess({ ownerOpenId: "owner-1" }, "guest-1")).toEqual({
      accept: false,
      reason: "unauthorized",
    });
  });

  it("accepts allowlist members even without an owner binding", () => {
    expect(decideQQAccess({ allowlist: ["a", "b"] }, "b")).toEqual({
      accept: true,
      mode: "allowlist",
      bindRuntime: false,
    });
  });

  it("normalizes and deduplicates allowlist values", () => {
    expect(normalizeQQAllowlist([" a ", "", "b", "a", "   "])).toEqual(["a", "b"]);
  });

  it("describes and redacts access status for status surfaces", () => {
    expect(describeQQAccess({})).toBe("open (unbound)");
    expect(describeQQAccess({ runtimeBoundOpenId: "abcdefghijklmnop" })).toContain(
      "first-sender (runtime only, abcdef...mnop)",
    );
    expect(describeQQAccess({ ownerOpenId: "abcdefghijklmnop", allowlist: ["x", "y"] })).toBe(
      "owner abcdef...mnop, allowlist 2",
    );
    expect(redactQQOpenId("short-id")).toBe("short-id");
  });
});
