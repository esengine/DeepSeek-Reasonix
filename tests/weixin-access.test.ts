import { describe, expect, it } from "vitest";
import { decideWeixinAccess, describeWeixinAccess } from "../src/weixin/access.js";

describe("Weixin access control", () => {
  it("fails closed when no owner or allowlist is configured", () => {
    expect(decideWeixinAccess({}, "wx-user-1")).toEqual({
      accept: false,
      reason: "unauthorized",
    });
    expect(describeWeixinAccess({})).toBe("access control required");
  });

  it("accepts allowlist members without binding the first sender", () => {
    expect(decideWeixinAccess({ allowlist: ["wx-user-1", "wx-user-2"] }, "wx-user-2")).toEqual({
      accept: true,
      mode: "allowlist",
      bindRuntime: false,
    });
  });

  it("rejects non-matching senders once an owner is configured", () => {
    expect(decideWeixinAccess({ ownerUserId: "wx-user-1" }, "wx-user-2")).toEqual({
      accept: false,
      reason: "unauthorized",
    });
  });
});
