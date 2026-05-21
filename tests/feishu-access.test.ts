import { describe, expect, it } from "vitest";
import {
  decideFeishuAccess,
  describeFeishuAccess,
  normalizeFeishuAllowlist,
  redactFeishuOpenId,
} from "../src/feishu/access.js";

describe("Feishu access control", () => {
  it("binds the first sender for the current run when no persistent access rule exists", () => {
    const first = decideFeishuAccess({}, "ou-first");
    expect(first).toEqual({ accept: true, mode: "open", bindRuntime: true });

    const repeat = decideFeishuAccess({ runtimeBoundOpenId: "ou-first" }, "ou-first");
    expect(repeat).toEqual({ accept: true, mode: "runtime", bindRuntime: false });

    const other = decideFeishuAccess({ runtimeBoundOpenId: "ou-first" }, "ou-other");
    expect(other).toEqual({ accept: false, reason: "unauthorized" });
  });

  it("accepts the persistent owner and reject outsiders", () => {
    expect(decideFeishuAccess({ ownerOpenId: "owner-1" }, "owner-1")).toEqual({
      accept: true,
      mode: "owner",
      bindRuntime: false,
    });
    expect(decideFeishuAccess({ ownerOpenId: "owner-1" }, "guest-1")).toEqual({
      accept: false,
      reason: "unauthorized",
    });
  });

  it("accepts allowlist members even without an owner binding", () => {
    expect(decideFeishuAccess({ allowlist: ["a", "b"] }, "b")).toEqual({
      accept: true,
      mode: "allowlist",
      bindRuntime: false,
    });
  });

  it("normalizes and deduplicates allowlist values", () => {
    expect(normalizeFeishuAllowlist([" a ", "", "b", "a", "   "])).toEqual(["a", "b"]);
  });

  it("describes and redacts access status for status surfaces", () => {
    expect(describeFeishuAccess({})).toBe("open (unbound)");
    expect(describeFeishuAccess({ runtimeBoundOpenId: "ou_abcdefghijklmnop" })).toContain(
      "first-sender (runtime only, ou_abc...mnop)",
    );
    expect(
      describeFeishuAccess({ ownerOpenId: "ou_abcdefghijklmnop", allowlist: ["x", "y"] }),
    ).toBe("owner ou_abc...mnop, allowlist 2");
    expect(redactFeishuOpenId("short-id")).toBe("short-id");
  });
});
